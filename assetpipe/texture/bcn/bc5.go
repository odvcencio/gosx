package bcn

import (
	"fmt"
	"math"
	"sort"
)

// BC5Options controls the two-channel encoder.
type BC5Options struct {
	// Transfer must be TransferUnorm. BC5 has no sRGB VkFormat.
	Transfer Transfer
	// Quality trades encode time for image quality.
	Quality Quality
	// First picks the source channel of the first block. The zero value
	// picks red.
	First Channel
	// Second picks the source channel of the second block. The zero value
	// picks red, so a caller that wants the usual red and green pair sets
	// Second to ChannelG.
	Second Channel
	// Workers sets the goroutine count. Zero or one runs on the calling
	// goroutine, and a negative value asks for one worker for each
	// processor. The output never depends on this field.
	Workers int
}

// EncodeBC5 compresses two channels of s as BC5, sixteen bytes for each 4x4
// block.
//
// Bytes 0 to 7 hold a BC4 block of the First channel and bytes 8 to 15 hold a
// BC4 block of the Second channel. The two blocks are independent, so the
// function is two runs of the BC4 encoder.
//
// Use EncodeBC5Normal for a tangent-space normal map. This function leaves the
// channel values alone, which is what two unrelated data channels need.
func EncodeBC5(s *Surface, opts BC5Options) ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if opts.Transfer != TransferUnorm {
		return nil, fmt.Errorf("%w: BC5 has no sRGB VkFormat, so it needs TransferUnorm, got %s",
			ErrTransfer, opts.Transfer)
	}
	if opts.First < ChannelR || opts.First > ChannelA {
		return nil, fmt.Errorf("%w: first channel %d", ErrChannel, int(opts.First))
	}
	if opts.Second < ChannelR || opts.Second > ChannelA {
		return nil, fmt.Errorf("%w: second channel %d", ErrChannel, int(opts.Second))
	}
	tuning := bc4TuningFor(opts.Quality)
	return encodeBlocks(s, 16, opts.Workers, func(bx, by int, dst []byte) {
		var first, second [16]float64
		s.gatherChannel(bx, by, opts.First, &first)
		s.gatherChannel(bx, by, opts.Second, &second)
		encodeBC4Block(&first, tuning, dst[0:8])
		encodeBC4Block(&second, tuning, dst[8:16])
	}), nil
}

// EncodeBC5Normal compresses a tangent-space normal map as BC5.
//
// The function reads the three colour channels of each texel as a normal in the
// usual n*0.5+0.5 encoding, normalizes it, and stores the x and y parts. It does
// not touch the surface. A shader rebuilds the third part:
//
//	z = sqrt(max(0, 1 - x*x - y*y))
//
// Normalizing first matters. Any resample wider than a box filter pulls texels
// off the unit sphere, and a shorter vector shrinks x and y toward the middle
// code. The shader then rebuilds a z that is too large, which flattens the
// surface. Normalizing per texel before quantizing removes that error, and it
// costs one square root for each texel.
//
// Measure the result with AngularErrorBC5, not with a per-channel error. Near a
// silhouette, x or y sits close to 1 and z sits close to 0, so one code of
// channel error turns the normal by several degrees. A per-channel measurement
// hides that.
func EncodeBC5Normal(s *Surface, opts BC5Options) ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if opts.Transfer != TransferUnorm {
		return nil, fmt.Errorf("%w: a normal map is data, so it needs TransferUnorm, got %s",
			ErrTransfer, opts.Transfer)
	}
	tuning := bc4TuningFor(opts.Quality)
	return encodeBlocks(s, 16, opts.Workers, func(bx, by int, dst []byte) {
		var xs, ys [16]float64
		s.gatherNormal(bx, by, &xs, &ys)
		encodeBC4Block(&xs, tuning, dst[0:8])
		encodeBC4Block(&ys, tuning, dst[8:16])
	}), nil
}

// AngularError reports how far the rebuilt normals turned away from the source
// normals. Every field holds degrees.
type AngularError struct {
	// MeanDegrees is the arithmetic mean over every texel.
	MeanDegrees float64
	// P95Degrees is the 95th percentile. It shows the silhouette texels that
	// a mean hides.
	P95Degrees float64
	// MaxDegrees is the worst texel.
	MaxDegrees float64
}

// AngularErrorBC5 measures the angle between the source normals of s and the
// normals a shader rebuilds from a BC5 payload.
//
// The function normalizes each source texel first, because EncodeBC5Normal
// normalizes too, so the measurement reports the loss of the format and not the
// drift of the source.
//
// The rebuilt normal comes from the payload the same way a shader gets it:
//
//	x = 2*red - 1
//	y = 2*green - 1
//	z = sqrt(max(0, 1 - x*x - y*y))
//
// A source normal whose z part is negative can never match, because the rebuild
// always returns a non-negative z. Tangent-space normals point out of the
// surface, so a negative z means the source is wrong, not the encoder.
func AngularErrorBC5(s *Surface, data []byte) (AngularError, error) {
	if err := s.validate(); err != nil {
		return AngularError{}, err
	}
	codes, err := Decode(FormatBC5, data, s.Width, s.Height)
	if err != nil {
		return AngularError{}, err
	}
	angles := make([]float64, 0, s.Width*s.Height)
	for pixel := 0; pixel < s.Width*s.Height; pixel++ {
		i := pixel * 4
		sx, sy, sz := decodeNormal(s.Pix[i], s.Pix[i+1], s.Pix[i+2])
		dx := float64(codes[i])/255*2 - 1
		dy := float64(codes[i+1])/255*2 - 1
		dz := math.Sqrt(math.Max(0, 1-dx*dx-dy*dy))
		length := math.Sqrt(dx*dx + dy*dy + dz*dz)
		if length > 0 {
			dx, dy, dz = dx/length, dy/length, dz/length
		}
		cosine := math.Min(1, math.Max(-1, sx*dx+sy*dy+sz*dz))
		angles = append(angles, math.Acos(cosine)*180/math.Pi)
	}
	if len(angles) == 0 {
		return AngularError{}, nil
	}
	total := 0.0
	for _, a := range angles {
		total += a
	}
	sort.Float64s(angles)
	index := int(0.95 * float64(len(angles)-1))
	return AngularError{
		MeanDegrees: total / float64(len(angles)),
		P95Degrees:  angles[index],
		MaxDegrees:  angles[len(angles)-1],
	}, nil
}
