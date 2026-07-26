package bc7

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
)

// Errors the package returns.
var (
	// ErrShape reports a width, a height, or a pixel slice the encoder cannot
	// use.
	ErrShape = errors.New("bc7: invalid image shape")
	// ErrColorSpace reports a missing Options.Space. The package refuses to
	// guess, because the wrong guess darkens every colour texture and no test
	// downstream of the guess can tell.
	ErrColorSpace = errors.New("bc7: Options.Space must be Linear or SRGB")
	// ErrNoModes reports an Options.Modes mask that enables nothing.
	ErrNoModes = errors.New("bc7: Options.Modes enables no mode")
	// ErrRDOUnsupported reports a non-zero RDO setting. The package does not
	// implement rate-distortion optimization yet.
	ErrRDOUnsupported = errors.New("bc7: rate-distortion optimization is not implemented")
)

// Source is a linear-light RGBA float32 image.
//
// The layout matches assetpipe/texture.Image field for field: Pix holds four
// values per pixel in row-major order, so it holds Width*Height*4 values. The
// caller passes texture.Image.Pix straight through. This package cannot name
// texture.Image, because texture imports this package.
type Source struct {
	Width, Height int
	Pix           []float32
}

// Quality picks the speed and quality trade. The zero value is QualityBalanced,
// which is the level to use unless a measurement says otherwise.
type Quality int

const (
	// QualityBalanced enables all eight modes, evaluates four candidate
	// partitions per multi-subset mode, refines twice, and tries all four
	// channel rotations. On the package's coverage images it lands within
	// 0.25 dB of QualityBest at about a third of the cost.
	QualityBalanced Quality = iota
	// QualityFast enables modes 1, 5 and 6, evaluates two candidate
	// partitions, and refines once. It still tries all four channel rotations,
	// because measurement says they are the cheapest large gain in the whole
	// encoder. Use it for a preview build.
	QualityFast
	// QualityBest enables all eight modes, evaluates sixteen candidate
	// partitions, refines four times, measures every parity bit assignment
	// with a full index pass, and checks the neighbours of every projected
	// index. It is the reference the other levels are measured against, and it
	// costs roughly ten times QualityFast. Use it for a final bake.
	QualityBest
)

// String names the quality level for a metric or a log line.
func (q Quality) String() string {
	switch q {
	case QualityFast:
		return "fast"
	case QualityBest:
		return "best"
	}
	return "balanced"
}

// ModeMask selects which BC7 modes the encoder may pick. Zero means the quality
// level decides.
type ModeMask uint16

// One bit per BC7 mode, plus the two useful groupings.
const (
	Mode0 ModeMask = 1 << 0
	Mode1 ModeMask = 1 << 1
	Mode2 ModeMask = 1 << 2
	Mode3 ModeMask = 1 << 3
	Mode4 ModeMask = 1 << 4
	Mode5 ModeMask = 1 << 5
	Mode6 ModeMask = 1 << 6
	Mode7 ModeMask = 1 << 7

	// ModesAll enables every mode.
	ModesAll ModeMask = Mode0 | Mode1 | Mode2 | Mode3 | Mode4 | Mode5 | Mode6 | Mode7
	// ModesOpaque enables the modes that store no alpha, plus mode 6. Use it
	// when the source has no alpha channel and the extra modes only cost time.
	ModesOpaque ModeMask = Mode0 | Mode1 | Mode2 | Mode3 | Mode6
)

// RDOOptions reserves the rate-distortion optimization hook.
//
// A rate-distortion pass nudges each block towards a byte pattern that the
// following entropy coder, zlib in a KTX2 container or the Zstandard stage of a
// supercompressed one, codes cheaply. It trades a little peak signal-to-noise
// ratio for a large drop in supercompressed size. bc7enc_rdo and
// "toktx --uastc_rdo_l" both work that way.
//
// The package does not implement it, and Encode rejects a non-zero Lambda
// rather than ignoring it. The hook belongs between mode selection and packing:
// encodeBlock already ranks several candidates per block by measured error, so
// an RDO pass keeps the top few instead of one, then picks per block by
// error + Lambda * estimated bit cost, with the cost estimated from the blocks
// already emitted. Adding it needs no change to this API.
type RDOOptions struct {
	// Lambda weights bit cost against squared error. Zero disables the pass.
	Lambda float64
}

// Options controls one encode.
type Options struct {
	// Space says whether the stored codes carry the sRGB transfer function.
	// The field has no default: Encode returns ErrColorSpace for the zero
	// value. Pair the choice with the matching VkFormat, which
	// ColorSpace.VkFormat returns.
	Space ColorSpace
	// Quality picks the speed and quality trade.
	Quality Quality
	// Modes overrides the mode set of the quality level. Zero keeps the
	// level's own set.
	Modes ModeMask
	// ChannelWeights scales the squared error of red, green, blue and alpha
	// during mode and endpoint selection. The zero value means uniform, which
	// maximizes the peak signal-to-noise ratio. Weight green higher to trade
	// that measure for perceived quality.
	ChannelWeights [4]float64
	// Parallel bounds the goroutines the encoder runs. Zero means
	// runtime.GOMAXPROCS, and one means no extra goroutine.
	Parallel int
	// RDO reserves the rate-distortion hook. It must stay zero.
	RDO RDOOptions
}

// Stats reports what one encode did. Every number is measured, not estimated:
// the error comes from decoding each packed block with DecodeBlock and comparing
// it with the source texels.
type Stats struct {
	// Blocks counts the blocks written.
	Blocks int
	// ModeCounts holds the block count per BC7 mode.
	ModeCounts [8]int
	// SquaredError totals the squared difference over every channel of every
	// texel, in 8-bit code units, in the space Options.Space names. Padding
	// texels of a block that overhangs the image are included, because the GPU
	// samples them at the edge.
	SquaredError float64
	// Samples counts the channel samples SquaredError covers.
	Samples int
}

// MSE returns the mean squared error per channel sample.
func (s Stats) MSE() float64 {
	if s.Samples == 0 {
		return 0
	}
	return s.SquaredError / float64(s.Samples)
}

// PSNR returns the peak signal-to-noise ratio in decibels, against a peak of
// 255 code values.
//
// The measure runs on the stored 8-bit codes, so an sRGB texture is measured
// after the transfer function. That is the right space for a colour map: the
// sRGB curve spends more codes on the dark end, where the eye is more sensitive,
// so equal linear error is not equal perceived error.
//
// A perfect encode returns math.Inf(1).
func (s Stats) PSNR() float64 {
	mse := s.MSE()
	if mse <= 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/mse)
}

// BlocksFor returns the block counts across and down for one image size.
func BlocksFor(width, height int) (int, int) {
	if width < 1 || height < 1 {
		return 0, 0
	}
	return (width + 3) / 4, (height + 3) / 4
}

// EncodedSize returns the byte count Encode produces for one image size. BC7
// costs exactly one byte per texel once the size is a multiple of four.
func EncodedSize(width, height int) int {
	bx, by := BlocksFor(width, height)
	return bx * by * BlockBytes
}

// Encode compresses one image to BC7 blocks.
//
// The result holds the blocks in row-major block order, 16 bytes each, which is
// the layout KTX2 and DDS both expect. An image whose width or height is not a
// multiple of four pads the last block by repeating the edge texel.
func Encode(src Source, opts Options) ([]byte, error) {
	data, _, err := EncodeWithStats(src, opts)
	return data, err
}

// EncodeWithStats compresses one image and measures the result.
//
// The measurement decodes every block it just packed. That costs a few percent
// of the encode, and it means a disagreement between the packer and the decoder
// shows up as a bad number instead of a plausible image.
func EncodeWithStats(src Source, opts Options) ([]byte, Stats, error) {
	cfg, err := resolve(opts)
	if err != nil {
		return nil, Stats{}, err
	}
	if src.Width < 1 || src.Height < 1 {
		return nil, Stats{}, fmt.Errorf("%w: %dx%d", ErrShape, src.Width, src.Height)
	}
	want := src.Width * src.Height * 4
	if len(src.Pix) < want {
		return nil, Stats{}, fmt.Errorf("%w: Pix holds %d values, want %d", ErrShape, len(src.Pix), want)
	}

	data, stats := encodeImage(src, cfg, opts.Parallel)
	return data, stats, nil
}

// encodeImage runs the block loop. The tests call it with a deliberately
// degraded config, so the mutation checks travel the production path.
func encodeImage(src Source, cfg config, parallel int) ([]byte, Stats) {
	bx, by := BlocksFor(src.Width, src.Height)
	out := make([]byte, bx*by*BlockBytes)

	workers := parallel
	if workers < 1 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > by {
		workers = by
	}

	partials := make([]Stats, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			sc := &scratch{}
			st := &partials[worker]
			for row := worker; row < by; row += workers {
				for col := 0; col < bx; col++ {
					loadBlock(sc, &src, cfg.space, col, row)
					cand := encodeBlock(sc, &cfg)
					packed := packBlock(&cand.spec)
					off := (row*bx + col) * BlockBytes
					copy(out[off:], packed[:])

					st.Blocks++
					st.ModeCounts[cand.spec.mode]++
					got := DecodeBlock(packed[:])
					for t := 0; t < 16; t++ {
						for c := 0; c < 4; c++ {
							d := float64(int(got[t][c]) - int(sc.block[t][c]))
							st.SquaredError += d * d
						}
					}
					st.Samples += 64
				}
			}
		}(w)
	}
	wg.Wait()

	var total Stats
	for _, p := range partials {
		total.Blocks += p.Blocks
		total.SquaredError += p.SquaredError
		total.Samples += p.Samples
		for i := range p.ModeCounts {
			total.ModeCounts[i] += p.ModeCounts[i]
		}
	}
	return out, total
}

// Decode expands BC7 blocks back to linear-light RGBA float32.
//
// space must match the space the encoder used, because Decode inverts the same
// transfer function the GPU sampler would.
func Decode(data []byte, width, height int, space ColorSpace) (Source, error) {
	if space != Linear && space != SRGB {
		return Source{}, ErrColorSpace
	}
	if width < 1 || height < 1 {
		return Source{}, fmt.Errorf("%w: %dx%d", ErrShape, width, height)
	}
	bx, by := BlocksFor(width, height)
	if len(data) < bx*by*BlockBytes {
		return Source{}, fmt.Errorf("%w: %d bytes, want %d", ErrShape, len(data), bx*by*BlockBytes)
	}
	out := Source{Width: width, Height: height, Pix: make([]float32, width*height*4)}
	for row := 0; row < by; row++ {
		for col := 0; col < bx; col++ {
			off := (row*bx + col) * BlockBytes
			texels := DecodeBlock(data[off : off+BlockBytes])
			for t := 0; t < 16; t++ {
				x := col*4 + t%4
				y := row*4 + t/4
				if x >= width || y >= height {
					continue
				}
				linear := decodeTexel(space, texels[t])
				i := (y*width + x) * 4
				copy(out.Pix[i:i+4], linear[:])
			}
		}
	}
	return out, nil
}

// resolve turns public options into the internal setting and rejects a bad one.
func resolve(opts Options) (config, error) {
	if opts.Space != Linear && opts.Space != SRGB {
		return config{}, ErrColorSpace
	}
	if opts.RDO.Lambda != 0 {
		return config{}, ErrRDOUnsupported
	}
	cfg := config{space: opts.Space, seed: seedPCA}
	// Every number below comes from a measurement, not from a guess. See
	// TestReportEncoderSteps for the table each one was picked from.
	switch opts.Quality {
	case QualityFast:
		cfg.modes = Mode1 | Mode5 | Mode6
		cfg.partitions = 2
		cfg.rounds = 1
		cfg.rotations = 4
	case QualityBest:
		cfg.modes = ModesAll
		cfg.partitions = 16
		cfg.rounds = 4
		cfg.rotations = 4
		cfg.exhaustiveParity = true
		cfg.exactIndices = true
	default:
		cfg.modes = ModesAll
		cfg.partitions = 4
		cfg.rounds = 2
		cfg.rotations = 4
	}
	if opts.Modes != 0 {
		cfg.modes = opts.Modes & ModesAll
		if cfg.modes == 0 {
			return config{}, ErrNoModes
		}
	}
	cfg.weights = opts.ChannelWeights
	if cfg.weights == [4]float64{} {
		cfg.weights = [4]float64{1, 1, 1, 1}
	}
	for c, w := range cfg.weights {
		if w < 0 || math.IsNaN(w) {
			return config{}, fmt.Errorf("%w: ChannelWeights[%d] is %v", ErrShape, c, w)
		}
	}
	return cfg, nil
}

// loadBlock copies one 4 by 4 block out of the image and applies the transfer
// function.
//
// A block that overhangs the image repeats the edge texel. Repeating beats
// filling with black: the GPU clamps at the edge when it samples, so a black
// pad would bleed into the last real texel through the block's shared endpoints.
func loadBlock(sc *scratch, src *Source, space ColorSpace, col, row int) {
	for t := 0; t < 16; t++ {
		x := col*4 + t%4
		y := row*4 + t/4
		if x >= src.Width {
			x = src.Width - 1
		}
		if y >= src.Height {
			y = src.Height - 1
		}
		i := (y*src.Width + x) * 4
		px := encodeTexel(space, src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3])
		for c := 0; c < 4; c++ {
			sc.block[t][c] = int32(px[c])
		}
	}
}
