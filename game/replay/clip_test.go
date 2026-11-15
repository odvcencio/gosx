package replay_test

import (
	"testing"

	"m31labs.dev/gosx/game/replay"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/sim/simcore"
)

func TestMarkersAndHighlightClip(t *testing.T) {
	g := newGame()
	rec := replay.NewRecorder(g, replay.Config{Interval: 40, TickRate: rate})
	hashes := []uint64{replay.Hash(g)}
	var m replay.Marker
	for i := 0; i < 300; i++ {
		rec.Tick(map[string]simcore.Input{mode.PlayerID(i % seats): {Data: []byte{byte(i % 32)}}})
		hashes = append(hashes, replay.Hash(g))
		if i == 149 {
			m = rec.Mark("goal")
		}
	}
	if m.Tick != 150 || m.Label != "goal" {
		t.Fatalf("marker %+v", m)
	}
	full := rec.Recording()
	clip, err := full.Highlight(m, 70, 45)
	if err != nil {
		t.Fatal(err)
	}
	if clip.Start() != 80 || clip.End() != 195 {
		t.Fatalf("clip range [%d,%d]", clip.Start(), clip.End())
	}
	if ms := clip.Markers(); len(ms) != 1 || ms[0] != m {
		t.Fatalf("clip markers %v", ms)
	}
	if clip.Bytes() >= full.Bytes() {
		t.Fatalf("clip %d bytes not smaller than full %d", clip.Bytes(), full.Bytes())
	}

	// Ship it: bytes -> a fresh process -> a fresh game.
	data := clip.Marshal()
	got, err := replay.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	g2 := newGame()
	p, err := replay.NewPlayer(g2, got)
	if err != nil {
		t.Fatal(err)
	}
	for n := got.Start(); n <= got.End(); n += 7 {
		if err := p.Seek(n); err != nil || replay.Hash(g2) != hashes[n] {
			t.Fatalf("tick %d: err %v, hash mismatch", n, err)
		}
	}
	_ = p.Seek(got.End())
	if replay.Hash(g2) != hashes[195] {
		t.Fatal("end of clip differs")
	}
	if err := p.Seek(79); err == nil {
		t.Fatal("seek before the clip succeeded")
	}
	if err := p.Seek(196); err == nil {
		t.Fatal("seek after the clip succeeded")
	}
	t.Logf("clip of %d ticks: %d bytes", got.End()-got.Start(), len(data))
}

func TestClipOnKeyframeBoundaries(t *testing.T) {
	rec, hashes := play(t, 120, replay.Config{Interval: 30})
	full := rec.Recording()
	for _, c := range [][2]uint64{{30, 60}, {0, 120}, {59, 61}, {119, 120}, {60, 60}} {
		clip, err := full.Clip(c[0], c[1])
		if err != nil {
			t.Fatal(err)
		}
		got, err := replay.Unmarshal(clip.Marshal())
		if err != nil {
			t.Fatalf("%v: %v", c, err)
		}
		g := newGame()
		p, err := replay.NewPlayer(g, got)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []uint64{c[0], c[1], (c[0] + c[1]) / 2} {
			if err := p.Seek(n); err != nil || replay.Hash(g) != hashes[n] {
				t.Fatalf("clip %v tick %d: %v", c, n, err)
			}
		}
	}
	if _, err := full.Clip(100, 50); err == nil {
		t.Fatal("empty range accepted")
	}
}

func TestMarkersDropWithEvictedTicks(t *testing.T) {
	g := newGame()
	rec := replay.NewRecorder(g, replay.Config{Interval: 20, MaxBytes: 4 << 10})
	rec.Mark("early")
	for i := 0; i < 3000; i++ {
		rec.Tick(nil)
	}
	rec.Mark("late")
	ms := rec.Recording().Markers()
	if len(ms) != 1 || ms[0].Label != "late" {
		t.Fatalf("markers %v", ms)
	}
}

func TestUnmarshalRejectsDamage(t *testing.T) {
	rec, _ := play(t, 100, replay.Config{Interval: 25})
	rec.Mark("x")
	data := rec.Recording().Marshal()
	for _, bad := range [][]byte{nil, data[:10], data[:len(data)-1]} {
		if _, err := replay.Unmarshal(bad); err == nil {
			t.Fatal("accepted truncated data")
		}
	}
	for _, i := range []int{3, 20, len(data) / 2, len(data) - 12} {
		d := append([]byte(nil), data...)
		d[i] ^= 0x40
		if _, err := replay.Unmarshal(d); err == nil {
			t.Fatalf("accepted a flipped byte at %d", i)
		}
	}
}

func FuzzUnmarshal(f *testing.F) {
	rec, _ := play(f, 60, replay.Config{Interval: 20})
	f.Add(rec.Recording().Marshal())
	f.Add([]byte("GXRPLY\x00\x00"))
	f.Fuzz(func(t *testing.T, b []byte) {
		r, err := replay.Unmarshal(b)
		if err != nil {
			return
		}
		g := newGame()
		if p, err := replay.NewPlayer(g, r); err == nil {
			p.StepForward(5)
		}
	})
}
