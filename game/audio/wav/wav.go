// Package wav writes PCM WAV files from Go samples and prepares sound that
// loops: what a game needs to synthesize a cue in Go and hand it to
// audio.Player (through audio.Library and audio.FetchBytes) or to bake it to
// a file.
package wav

import (
	"encoding/binary"
	"math"
)

// PCM16 encodes frames sample frames of 16-bit PCM with the given number of
// interleaved channels at rate samples a second. sample returns frame f of
// channel c.
func PCM16(channels, rate, frames int, sample func(f, c int) int16) []byte {
	block := channels * 2
	data := make([]byte, 44+frames*block)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1) // PCM
	binary.LittleEndian.PutUint16(data[22:], uint16(channels))
	binary.LittleEndian.PutUint32(data[24:], uint32(rate))
	binary.LittleEndian.PutUint32(data[28:], uint32(rate*block))
	binary.LittleEndian.PutUint16(data[32:], uint16(block))
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(len(data)-44))
	for f := 0; f < frames; f++ {
		for c := 0; c < channels; c++ {
			binary.LittleEndian.PutUint16(data[44+f*block+c*2:], uint16(sample(f, c)))
		}
	}
	return data
}

// LoopJoin makes channels loop without a click: it blends the first n samples
// into the last n with an equal-power crossfade (the two ends are unrelated
// sound, so their power, not their amplitude, is kept), and returns the
// channels without their first n samples. Every channel must be longer than n.
func LoopJoin(n int, channels ...[]float64) [][]float64 {
	out := make([][]float64, len(channels))
	for k, ch := range channels {
		last := len(ch) - n
		for i := 0; i < n; i++ {
			a := float64(i) / float64(n) * math.Pi / 2
			ch[last+i] = ch[last+i]*math.Cos(a) + ch[i]*math.Sin(a)
		}
		out[k] = ch[n:]
	}
	return out
}

// FadeTail fades the last n samples of each channel linearly to silence, so a
// short cue ends without a click.
func FadeTail(n int, channels ...[]float64) {
	for _, ch := range channels {
		m := min(n, len(ch))
		for i := 0; i < m; i++ {
			ch[len(ch)-1-i] *= float64(i) / float64(m)
		}
	}
}

// Frames is the number of sample frames in a WAV that PCM16 made with the
// given number of channels.
func Frames(wav []byte, channels int) int { return (len(wav) - 44) / (2 * channels) }

// Repeat returns a WAV that PCM16 made, played k times over, as one WAV: for
// a loop that must be resampled with its join intact, say.
func Repeat(wav []byte, k int) []byte {
	data := wav[44:]
	out := make([]byte, 44, 44+k*len(data))
	copy(out, wav[:44])
	for range k {
		out = append(out, data...)
	}
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	binary.LittleEndian.PutUint32(out[40:], uint32(len(out)-44))
	return out
}
