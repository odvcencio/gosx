package wav

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestPCM16WritesAStereoHeaderAndInterleavedSamples(t *testing.T) {
	data := PCM16(2, 44100, 3, func(f, c int) int16 { return int16(f*10 + c) })
	if string(data[:4]) != "RIFF" || string(data[8:16]) != "WAVEfmt " || string(data[36:40]) != "data" {
		t.Fatal("not a RIFF WAVE file")
	}
	if got := binary.LittleEndian.Uint32(data[28:]); got != 44100*4 {
		t.Fatalf("byte rate %d", got)
	}
	if len(data) != 44+3*4 || int16(binary.LittleEndian.Uint16(data[44+2*4+2:])) != 21 {
		t.Fatalf("length %d or frame 2 channel 1 wrong", len(data))
	}
}

func TestLoopJoinKeepsPowerAcrossTheSeam(t *testing.T) {
	ch := []float64{1, 1, 1, 1, 0, 0, 0, 0}
	out := LoopJoin(4, ch)[0]
	if len(out) != 4 {
		t.Fatalf("len %d, want the first 4 samples dropped", len(out))
	}
	// out[i] = 0*cos + 1*sin: the start fades in under the end.
	for i, v := range out {
		if want := math.Sin(float64(i) / 4 * math.Pi / 2); math.Abs(v-want) > 1e-12 {
			t.Fatalf("out[%d] = %v, want %v", i, v, want)
		}
	}
}

func TestFadeTailEndsInSilence(t *testing.T) {
	ch := []float64{1, 1, 1, 1}
	FadeTail(2, ch)
	if ch[3] != 0 || ch[2] != .5 || ch[1] != 1 {
		t.Fatalf("faded = %v", ch)
	}
}

func TestRepeatKeepsAValidHeader(t *testing.T) {
	one := PCM16(2, 22050, 5, func(f, c int) int16 { return int16(f) })
	three := Repeat(one, 3)
	if Frames(three, 2) != 15 || binary.LittleEndian.Uint32(three[40:]) != 60 || binary.LittleEndian.Uint32(three[4:]) != uint32(len(three)-8) {
		t.Fatalf("frames %d, header %v", Frames(three, 2), three[:44])
	}
}
