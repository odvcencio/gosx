package webrtc

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/ice/v4"
	pion "github.com/pion/webrtc/v4"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/netplay"
)

// loopbackICE lets pion gather and use 127.0.0.1 candidates and skips mDNS, so
// two pion peers in one process connect without a network.
func loopbackICE(se *pion.SettingEngine) {
	se.SetIncludeLoopbackCandidate(true)
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	se.SetNetworkTypes([]pion.NetworkType{pion.NetworkTypeUDP4})
}

type rigPeers struct {
	server *Server
	client [2]*Peer // the players' ends
	relay  [2]*Peer // the relay's ends
}

// connectPlayers brings up a hub with a Server and connects two native
// clients through hub signaling.
func connectPlayers(t *testing.T, prefix string) *rigPeers {
	t.Helper()
	srv, err := NewServer(Config{ConfigureICE: loopbackICE})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	h := hub.New("netplay-" + prefix)
	srv.AttachHub(h)
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	url := "ws" + strings.TrimPrefix(ts.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r := &rigPeers{server: srv}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.client[i], errs[i] = DialHub(ctx, url, ClientConfig{Session: prefix + string(rune('a'+i)), ConfigureICE: loopbackICE})
		}()
	}
	for i := 0; i < 2; i++ {
		p, err := srv.Accept(ctx)
		if err != nil {
			t.Fatalf("accept %d: %v", i, err)
		}
		idx := int(p.Session()[len(p.Session())-1] - 'a')
		r.relay[idx] = p
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("client %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		for i := 0; i < 2; i++ {
			r.client[i].Close()
			r.relay[i].Close()
		}
	})
	return r
}

func TestChannelParameters(t *testing.T) {
	r := connectPlayers(t, "params")
	for i, p := range r.relay {
		if p.dc.Ordered() {
			t.Errorf("relay peer %d: channel is ordered", i)
		}
		mr := p.dc.MaxRetransmits()
		if mr == nil || *mr != 0 {
			t.Errorf("relay peer %d: maxRetransmits = %v, want 0", i, mr)
		}
		if !p.dc.Negotiated() || p.dc.ID() == nil || *p.dc.ID() != ChannelID {
			t.Errorf("relay peer %d: channel is not negotiated id %d", i, ChannelID)
		}
	}
}

func TestPeerRoundTripAndSizeLimit(t *testing.T) {
	r := connectPlayers(t, "rt")
	if err := r.client[0].Send(make([]byte, netplay.MaxPacketSize+1)); err != netplay.ErrPacketTooLarge {
		t.Fatalf("oversize send: %v", err)
	}
	want := []byte("hello relay")
	if err := r.client[0].Send(want); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if n, ok := r.relay[0].Recv(buf); ok {
			if string(buf[:n]) != string(want) {
				t.Fatalf("got %q", buf[:n])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("datagram never arrived")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMatchesThroughRelayHaveZeroDesyncs(t *testing.T) {
	if testing.Short() {
		t.Skip("real-time match")
	}
	for _, name := range []string{"m1", "m2"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runMatch(t, name, 240)
		})
	}
}

func runMatch(t *testing.T, name string, frames int64) {
	r := connectPlayers(t, name)
	runMatchOn(t, name, frames, [2]netplay.Transport{r.client[0], r.client[1]}, [2]netplay.Transport{r.relay[0], r.relay[1]})
}

// runMatchOn plays a real-time match between two sessions whose transports
// are players[i], with the relay on relayEnds[i].
func runMatchOn(t *testing.T, name string, frames int64, players, relayEnds [2]netplay.Transport) {
	relay, err := netplay.NewRelay(netplay.RelayConfig[duelPad]{
		NewGame: func() netplay.Game[duelPad] { return newDuelGame() },
		Codec:   duelCodec{}, Peers: relayEnds,
	})
	if err != nil {
		t.Fatal(err)
	}
	var games [2]*duelGame
	var sess [2]*netplay.Session[duelPad]
	var sums [2]map[int64]uint64
	for i := 0; i < 2; i++ {
		i := i
		games[i] = newDuelGame()
		sums[i] = map[int64]uint64{}
		sess[i], err = netplay.NewSession(netplay.Config[duelPad]{
			Game: games[i], Codec: duelCodec{}, Transport: players[i], LocalPlayer: i,
			InputDelay: 2, Relay: true,
			OnChecksum: func(f int64, s uint64) { sums[i][f] = s },
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	start := time.Now()
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	nextFrame := [2]time.Duration{}
	const frameDur = time.Second / 60
	deadline := start.Add(30 * time.Second)
	for sess[0].Frame() < frames || sess[1].Frame() < frames {
		<-tick.C
		now := time.Since(start)
		if time.Now().After(deadline) {
			t.Fatalf("match did not finish: frames %d and %d", sess[0].Frame(), sess[1].Frame())
		}
		relay.Tick(now)
		for i := 0; i < 2; i++ {
			if now < nextFrame[i] {
				continue
			}
			nextFrame[i] += frameDur
			f := sess[i].Frame()
			sess[i].Advance(now, duelInput(7, i, f))
			if err := sess[i].Err(); err != nil {
				t.Fatalf("session %d ended: %v", i, err)
			}
		}
	}

	rep := relay.Report()
	if len(rep.DesyncFrames) != 0 || rep.Resyncs != 0 {
		t.Fatalf("relay saw desyncs %v, resyncs %d", rep.DesyncFrames, rep.Resyncs)
	}
	common := 0
	for f, a := range sums[0] {
		if b, ok := sums[1][f]; ok {
			common++
			if a != b {
				t.Fatalf("checksum differs at frame %d", f)
			}
		}
	}
	if common < 4 {
		t.Fatalf("only %d common checksums", common)
	}
	if rep.VerifiedFrames < frames/2 {
		t.Fatalf("relay verified %d frames, want at least %d", rep.VerifiedFrames, frames/2)
	}
	t.Logf("%s: frames=%d common checksums=%d relay verified=%d rtt=%v rollbacks=%d",
		name, sess[0].Frame(), common, rep.VerifiedFrames, sess[0].RTT(), sess[0].Stats().Rollbacks)
}
