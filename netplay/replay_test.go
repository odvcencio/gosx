package netplay

import (
	"errors"
	"testing"
	"time"
)

func recordReplay(t *testing.T) (*Replay, [32]byte) {
	t.Helper()
	var id [32]byte
	id[0], id[31] = 0xab, 0xcd
	nc := NetConfig{Seed: 31, Delay: [2]time.Duration{30 * time.Millisecond, 30 * time.Millisecond}}
	r := newRig(t, rigOpts{links: [2]NetConfig{nc, nc}, id: id})
	r.run(8 * time.Second)
	rp := r.relay.Replay()
	if rp.Frames < 300 || len(rp.Checks) < 8 {
		t.Fatalf("replay too small: %d frames %d checks", rp.Frames, len(rp.Checks))
	}
	return rp, id
}

func TestReplayResimulatesToSameChecksums(t *testing.T) {
	rp, id := recordReplay(t)
	newGame := func() Game[pad] { return newFightGame() }
	if err := VerifyReplay(rp, id, newGame, padCodec{}); err != nil {
		t.Fatal(err)
	}
	// Through the binary format.
	back, err := UnmarshalReplay(rp.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyReplay(back, id, newGame, padCodec{}); err != nil {
		t.Fatal(err)
	}
	if back.Frames != rp.Frames || len(back.Checks) != len(rp.Checks) {
		t.Fatal("round trip changed the replay")
	}
}

func TestReplayRefusesDifferentIdentity(t *testing.T) {
	rp, id := recordReplay(t)
	id[5] ^= 1
	built := 0
	err := VerifyReplay(rp, id, func() Game[pad] { built++; return newFightGame() }, padCodec{})
	if !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("err = %v", err)
	}
	if built != 0 {
		t.Fatal("simulated a replay recorded under another identity")
	}
}

func TestReplayDetectsTamperedInputs(t *testing.T) {
	rp, id := recordReplay(t)
	rp.Inputs[len(rp.Inputs)/2] ^= byte(padAttack | padLeft)
	err := VerifyReplay(rp, id, func() Game[pad] { return newFightGame() }, padCodec{})
	if !errors.Is(err, ErrReplayDiverge) {
		t.Fatalf("err = %v, want divergence", err)
	}
	var d *ReplayDivergeError
	if !errors.As(err, &d) || d.Frame == 0 {
		t.Fatalf("no divergence frame: %v", err)
	}
}

func TestReplayFormatIsVersionedAndValidated(t *testing.T) {
	rp, _ := recordReplay(t)
	b := rp.Marshal()
	if string(b[:7]) != "GXNPRPL" || b[8] != 1 {
		t.Fatalf("header % x", b[:12])
	}
	bad := append([]byte(nil), b...)
	bad[8] = 2
	if _, err := UnmarshalReplay(bad); !errors.Is(err, ErrReplayVersion) {
		t.Fatalf("version: %v", err)
	}
	bad = append([]byte(nil), b...)
	bad[len(bad)/2] ^= 1
	if _, err := UnmarshalReplay(bad); !errors.Is(err, ErrReplayFormat) {
		t.Fatalf("corruption: %v", err)
	}
	for _, n := range []int{0, 7, 20, len(b) - 1} {
		if _, err := UnmarshalReplay(b[:n]); err == nil {
			t.Fatalf("accepted %d-byte truncation", n)
		}
	}
}
