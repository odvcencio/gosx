package replay_test

import (
	"testing"

	"m31labs.dev/gosx/game/replay"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/roomtest"
	"m31labs.dev/gosx/sim/simcore"
)

// goldenRun plays a fixed script and returns the state hash at ticks 0, 100,
// 500 and 1000 and the hash of the marshalled clip. Integer game math and a
// byte-exact clip format make these identical on every architecture, so the
// same test on arm64 and amd64 proves cross-platform replay.
func goldenRun() (hashes [4]uint64, clip uint64, rec *replay.Recording) {
	g := roomtest.NewGame(3, 8, 99, 60)
	rc := replay.NewRecorder(g, replay.Config{Interval: 90, TickRate: 60})
	at := map[int]int{0: 0, 100: 1, 500: 2, 1000: 3}
	if i, ok := at[0]; ok {
		hashes[i] = replay.Hash(g)
	}
	for n := 1; n <= 1000; n++ {
		rc.Tick(map[string]simcore.Input{
			mode.PlayerID(n % 3):       {Data: []byte{byte(n * 7 % 32)}},
			mode.PlayerID((n + 1) % 3): {Data: []byte{byte(n * 3 % 32)}},
		})
		if i, ok := at[n]; ok {
			hashes[i] = replay.Hash(g)
		}
		if n == 600 {
			rc.Mark("m")
		}
	}
	rec = rc.Recording()
	return hashes, replay.HashBytes(rec.Marshal()), rec
}

// Recorded on linux/amd64. If this fails on arm64, replay is not portable.
const (
	goldenH0    = 0xe90d56d9fb93c829
	goldenH100  = 0x7d36d5c302411189
	goldenH500  = 0x1d2c707947e720a7
	goldenH1000 = 0xaf408861fd746ec7
	goldenClip  = 0x38eff8576dc2b608
)

func TestGoldenAcrossArchitectures(t *testing.T) {
	hashes, clip, rec := goldenRun()
	want := [4]uint64{goldenH0, goldenH100, goldenH500, goldenH1000}
	if hashes != want || clip != goldenClip {
		t.Fatalf("golden mismatch on this architecture:\nhashes %#x\nclip   %#x", hashes, clip)
	}
	// Replay, from the decoded clip, reproduces the same states.
	dec, err := replay.Unmarshal(rec.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	g := roomtest.NewGame(3, 8, 99, 60)
	p, err := replay.NewPlayer(g, dec)
	if err != nil {
		t.Fatal(err)
	}
	for i, tick := range []uint64{1000, 100, 500, 0} {
		_ = i
		if err := p.Seek(tick); err != nil {
			t.Fatal(err)
		}
		idx := map[uint64]int{0: 0, 100: 1, 500: 2, 1000: 3}[tick]
		if got := replay.Hash(g); got != want[idx] {
			t.Fatalf("replayed tick %d: %#x, want %#x", tick, got, want[idx])
		}
	}
}
