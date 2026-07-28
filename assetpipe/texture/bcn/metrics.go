package bcn

import (
	"fmt"
	"math"
)

// PSNR8 returns the peak signal-to-noise ratio in decibels between two arrays of
// stored 8-bit codes.
//
// Both arrays hold four codes for each texel in row-major order, which is what
// ReferenceCodes and Decode both return. channels picks which components join the
// measurement; an empty list picks all four.
//
// Pick the channels on purpose. An opaque BC1 payload stores no alpha, so
// comparing alpha adds a perfect channel and lifts the number for nothing. A BC4
// payload stores red only, so comparing green and blue would report the error of
// two channels that carry no data.
//
// The peak is 255, one whole code range. The result is math.Inf(1) when the two
// arrays agree everywhere.
//
// The codes carry whatever transfer function the encoder applied. For a colour
// target they are sRGB codes, so the squared error is already weighted the way
// perception weights it: a code step near black covers less light than a code
// step near white. For a data target the codes are linear, which is the right
// space for a number that a shader reads as a number.
func PSNR8(reference, got []byte, channels ...Channel) (float64, error) {
	if len(reference) != len(got) {
		return 0, fmt.Errorf("%w: reference holds %d bytes, got %d", ErrPayload, len(reference), len(got))
	}
	if len(reference) == 0 || len(reference)%4 != 0 {
		return 0, fmt.Errorf("%w: %d bytes is not four codes for each texel", ErrShape, len(reference))
	}
	if len(channels) == 0 {
		channels = []Channel{ChannelR, ChannelG, ChannelB, ChannelA}
	}
	for _, ch := range channels {
		if ch < ChannelR || ch > ChannelA {
			return 0, fmt.Errorf("%w: %d", ErrChannel, int(ch))
		}
	}
	var total float64
	count := 0
	for base := 0; base < len(reference); base += 4 {
		for _, ch := range channels {
			d := float64(reference[base+int(ch)]) - float64(got[base+int(ch)])
			total += d * d
			count++
		}
	}
	if total == 0 {
		return math.Inf(1), nil
	}
	mse := total / float64(count)
	return 10 * math.Log10(255*255/mse), nil
}

// MaxAbsError returns the largest single-code difference over the chosen
// channels. A mean hides one bad texel, and this does not.
func MaxAbsError(reference, got []byte, channels ...Channel) (int, error) {
	if len(reference) != len(got) {
		return 0, fmt.Errorf("%w: reference holds %d bytes, got %d", ErrPayload, len(reference), len(got))
	}
	if len(channels) == 0 {
		channels = []Channel{ChannelR, ChannelG, ChannelB, ChannelA}
	}
	worst := 0
	for base := 0; base+3 < len(reference); base += 4 {
		for _, ch := range channels {
			d := int(reference[base+int(ch)]) - int(got[base+int(ch)])
			if d < 0 {
				d = -d
			}
			if d > worst {
				worst = d
			}
		}
	}
	return worst, nil
}
