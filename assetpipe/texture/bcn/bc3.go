package bcn

import "fmt"

// BC3Options controls the colour-plus-alpha encoder.
type BC3Options struct {
	// Transfer names the function the encoder applies to the three colour
	// channels. The alpha channel always stays linear, because the sRGB
	// specification applies its transfer function to colour only. The zero
	// value is illegal. Read the package doc.
	Transfer Transfer
	// Quality trades encode time for image quality.
	Quality Quality
	// Workers sets the goroutine count. Zero or one runs on the calling
	// goroutine, and a negative value asks for one worker for each
	// processor. The output never depends on this field.
	Workers int
}

// EncodeBC3 compresses s as BC3, sixteen bytes for each 4x4 block.
//
// Each block holds a BC4-layout alpha block in bytes 0 to 7 and a BC1-layout
// colour block in bytes 8 to 15. The colour block always decodes with the
// four-colour mode, whatever the order of its two endpoints is. So BC3 keeps all
// four colour entries and spends no entry on transparency, which is why BC3
// carries less colour error than a BC1 cutout block on the same texels.
func EncodeBC3(s *Surface, opts BC3Options) ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if !opts.Transfer.valid() {
		return nil, fmt.Errorf("%w: got %s", ErrTransfer, opts.Transfer)
	}
	colorTuning := bc1TuningFor(opts.Quality)
	alphaTuning := bc4TuningFor(opts.Quality)
	return encodeBlocks(s, 16, opts.Workers, func(bx, by int, dst []byte) {
		var alpha [16]float64
		s.gatherChannel(bx, by, ChannelA, &alpha)
		encodeBC4Block(&alpha, alphaTuning, dst[0:8])

		texels, _ := gatherColor(s, bx, by, opts.Transfer, false, 0)
		encodeColorBlock(&texels, 0, colorTuning, true, dst[8:16])
	}), nil
}
