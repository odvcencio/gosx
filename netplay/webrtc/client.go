package webrtc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	pion "github.com/pion/webrtc/v4"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/netplay/dgram"
	"m31labs.dev/gosx/netplay/hubnet"
	"m31labs.dev/gosx/netplay/signal"
)

// ClientConfig configures a native client (a bot, a test peer, or a Go
// process that is not a browser).
type ClientConfig struct {
	// Session labels the connection in signaling (default "netplay").
	Session string
	// ICEServers are used when the relay sends none.
	ICEServers []signal.ICEServer
	// ConfigureICE may adjust pion's setting engine. Optional.
	ConfigureICE func(*pion.SettingEngine)
	// QueueLen is the receive queue length in datagrams (default 128).
	QueueLen int
	// ConfigWait is how long to wait for the relay's ICE config before using
	// ICEServers alone (default 1 s).
	ConfigWait time.Duration
	// Header is sent with the WebSocket handshake by DialHub.
	Header http.Header
}

// Signaler is the signaling path of a client: it sends messages to the relay
// and delivers the relay's replies.
type Signaler interface {
	Send(signal.Message) error
	// Recv returns the channel of validated relay messages. The channel closes
	// when the signaling path ends.
	Recv() <-chan signal.Message
}

// Connect runs the offering side of the WebRTC handshake over sig and returns
// the open Peer. The relay must be answering through a Server.
func Connect(ctx context.Context, sig Signaler, cfg ClientConfig) (*Peer, error) {
	if cfg.Session == "" {
		cfg.Session = "netplay"
	}
	if cfg.ConfigWait == 0 {
		cfg.ConfigWait = time.Second
	}
	servers := cfg.ICEServers
	if err := sig.Send(&signal.Hello{Session: cfg.Session}); err != nil {
		return nil, err
	}
	// Wait for the relay's ICE configuration, but not forever.
	wait := time.NewTimer(cfg.ConfigWait)
	defer wait.Stop()
configured:
	for {
		select {
		case m, ok := <-sig.Recv():
			if !ok {
				return nil, errors.New("netplay/webrtc: signaling closed")
			}
			if c, isCfg := m.(*signal.Config); isCfg && c.Session == cfg.Session {
				if len(c.ICEServers) > 0 {
					servers = c.ICEServers
				}
				break configured
			}
		case <-wait.C:
			break configured
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	pc, err := newAPI(cfg.ConfigureICE).NewPeerConnection(pion.Configuration{ICEServers: pionServers(servers)})
	if err != nil {
		return nil, err
	}
	dc, err := pc.CreateDataChannel(ChannelLabel, channelInit())
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	peer := newPeer("", cfg.Session, pc, dc, cfg.QueueLen)
	opened := make(chan struct{})
	dc.OnOpen(func() { close(opened) })

	var mu sync.Mutex
	offered := false
	var pending []*signal.Candidate
	pc.OnICECandidate(func(c *pion.ICECandidate) {
		m := candidateMessage(cfg.Session, c)
		mu.Lock()
		if !offered {
			pending = append(pending, m)
			mu.Unlock()
			return
		}
		mu.Unlock()
		_ = sig.Send(m)
	})
	fail := func(err error) (*Peer, error) { _ = peer.shutdown(err); return nil, err }

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return fail(err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return fail(err)
	}
	if err := sig.Send(&signal.Offer{Session: cfg.Session, SDP: pc.LocalDescription().SDP}); err != nil {
		return fail(err)
	}
	mu.Lock()
	offered = true
	flush := pending
	pending = nil
	mu.Unlock()
	for _, m := range flush {
		_ = sig.Send(m)
	}

	remoteSet := false
	var early []*signal.Candidate
	for {
		select {
		case <-opened:
			return peer, nil
		case <-ctx.Done():
			return fail(ctx.Err())
		case m, ok := <-sig.Recv():
			if !ok {
				select {
				case <-opened:
					return peer, nil
				default:
				}
				return fail(errors.New("netplay/webrtc: signaling closed"))
			}
			switch m := m.(type) {
			case *signal.Answer:
				if m.Session != cfg.Session || remoteSet {
					continue
				}
				if err := pc.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: m.SDP}); err != nil {
					return fail(err)
				}
				remoteSet = true
				for _, c := range early {
					_ = pc.AddICECandidate(candidateInit(c))
				}
				early = nil
			case *signal.Candidate:
				if m.Session != cfg.Session || m.Candidate == "" {
					continue
				}
				if !remoteSet {
					if len(early) < maxRemoteCandidates {
						early = append(early, m)
					}
					continue
				}
				_ = pc.AddICECandidate(candidateInit(m))
			case *signal.Failure:
				if m.Session == cfg.Session {
					return fail(fmt.Errorf("netplay/webrtc: relay refused WebRTC: %s", m.Code))
				}
			}
		}
	}
}

// hubSignaler adapts a hub WebSocket to Signaler.
type hubSignaler struct {
	ws  *websocket.Conn
	wmu sync.Mutex
	in  chan signal.Message
	// bin holds fallback datagrams the relay sent in hub binary frames.
	bin *dgram.Queue
}

func (h *hubSignaler) Send(m signal.Message) error {
	ev, data, err := signal.Marshal(m)
	if err != nil {
		return err
	}
	h.wmu.Lock()
	defer h.wmu.Unlock()
	_ = h.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return h.ws.WriteJSON(hub.Message{Event: ev, Data: data})
}

func (h *hubSignaler) Recv() <-chan signal.Message { return h.in }

// sendBinary writes one fallback datagram as a hub binary frame.
func (h *hubSignaler) sendBinary(p []byte) error {
	frame := make([]byte, 1+len(p))
	frame[0] = hubnet.FrameTag
	copy(frame[1:], p)
	h.wmu.Lock()
	defer h.wmu.Unlock()
	_ = h.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return h.ws.WriteMessage(websocket.BinaryMessage, frame)
}

func (h *hubSignaler) readLoop() {
	defer close(h.in)
	for {
		typ, data, err := h.ws.ReadMessage()
		if err != nil {
			return
		}
		if typ == websocket.BinaryMessage {
			if len(data) > 1 && data[0] == hubnet.FrameTag {
				h.bin.Push(data[1:])
			}
			continue
		}
		var env hub.Message
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		m, err := signal.Parse(env.Event, env.Data)
		if err != nil {
			continue
		}
		select {
		case h.in <- m:
		default: // a slow consumer loses signaling, which the connect timeout covers
		}
	}
}

// DialHub opens the hub WebSocket at wsURL (ws:// or wss://), runs Connect
// over it and returns the open Peer. The WebSocket stays open until the Peer
// closes.
func DialHub(ctx context.Context, wsURL string, cfg ClientConfig) (*Peer, error) {
	ws, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, cfg.Header)
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(256 * 1024)
	sig := &hubSignaler{ws: ws, in: make(chan signal.Message, 64), bin: dgram.NewQueue(128)}
	go sig.readLoop()
	peer, err := Connect(ctx, sig, cfg)
	if err != nil {
		_ = ws.Close()
		return nil, err
	}
	peer.sig = sig
	prev := peer.onClose
	peer.onClose = func() {
		_ = ws.Close()
		if prev != nil {
			prev()
		}
	}
	return peer, nil
}
