package loudness

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// ParseWAV decodes a RIFF WAVE file of 16-bit or 24-bit integer or 32-bit
// float PCM, mixing channels down to mono.
func ParseWAV(data []byte) (Audio, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return Audio{}, errors.New("loudness: not a RIFF WAVE file")
	}
	var format, channels, bits uint16
	var rate uint32
	var pcm []byte
	for i := 12; i+8 <= len(data); {
		id, size := string(data[i:i+4]), int(binary.LittleEndian.Uint32(data[i+4:]))
		if i+8+size > len(data) || size < 0 {
			// Streamed WAV leaves the size unknown; data runs to the end.
			if id != "data" {
				return Audio{}, errors.New("loudness: truncated WAV chunk")
			}
			size = len(data) - i - 8
		}
		body := data[i+8 : i+8+size]
		switch id {
		case "fmt ":
			if size < 16 {
				return Audio{}, errors.New("loudness: short fmt chunk")
			}
			format, channels = binary.LittleEndian.Uint16(body), binary.LittleEndian.Uint16(body[2:])
			rate, bits = binary.LittleEndian.Uint32(body[4:]), binary.LittleEndian.Uint16(body[14:])
			if format == 0xfffe && size >= 26 {
				format = binary.LittleEndian.Uint16(body[24:])
			}
		case "data":
			pcm = body
		}
		i += 8 + size + size&1
	}
	if channels == 0 || rate == 0 || pcm == nil {
		return Audio{}, errors.New("loudness: WAV lacks fmt or data")
	}
	width := int(bits) / 8
	if width == 0 {
		return Audio{}, errors.New("loudness: WAV has zero bits per sample")
	}
	frames := len(pcm) / (width * int(channels))
	out := Audio{Rate: int(rate), Samples: make([]float64, frames)}
	for f := 0; f < frames; f++ {
		var sum float64
		for c := 0; c < int(channels); c++ {
			p := pcm[(f*int(channels)+c)*width:]
			switch {
			case format == 3 && bits == 32:
				sum += float64(math.Float32frombits(binary.LittleEndian.Uint32(p)))
			case format == 1 && bits == 16:
				sum += float64(int16(binary.LittleEndian.Uint16(p))) / 32768
			case format == 1 && bits == 24:
				n := int32(p[0]) | int32(p[1])<<8 | int32(p[2])<<16
				sum += float64(n<<8>>8) / 8388608
			default:
				return Audio{}, fmt.Errorf("loudness: unsupported WAV format %d with %d bits", format, bits)
			}
		}
		out.Samples[f] = sum / float64(channels)
	}
	return out, nil
}

// PCM16 converts little-endian signed 16-bit mono samples to Audio.
func PCM16(data []byte, rate int) Audio {
	n := len(data) / 2
	out := Audio{Rate: rate, Samples: make([]float64, n)}
	for i := 0; i < n; i++ {
		out.Samples[i] = float64(int16(binary.LittleEndian.Uint16(data[i*2:]))) / 32768
	}
	return out
}

// Quantize16 converts samples to little-endian signed 16-bit, rounding and
// clamping.
func Quantize16(samples []float64) []byte {
	out := make([]byte, len(samples)*2)
	for i, v := range samples {
		q := math.Round(v * 32767)
		q = math.Max(-32768, math.Min(32767, q))
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(q)))
	}
	return out
}

// WAV16 encodes mono samples as a 16-bit PCM WAV file.
func WAV16(a Audio) []byte {
	pcm := Quantize16(a.Samples)
	data := make([]byte, 44+len(pcm))
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], uint32(a.Rate))
	binary.LittleEndian.PutUint32(data[28:], uint32(a.Rate*2))
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(len(pcm)))
	copy(data[44:], pcm)
	return data
}
