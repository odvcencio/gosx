package bcn

import "errors"

// Errors the package returns.
var (
	// ErrShape reports a surface size or pixel slice the encoder cannot use.
	ErrShape = errors.New("bcn: invalid surface shape")
	// ErrTransfer reports a missing or illegal transfer function. The zero
	// value of Transfer is illegal on purpose. See the package doc.
	ErrTransfer = errors.New("bcn: transfer function not named")
	// ErrChannel reports a channel index outside 0 to 3.
	ErrChannel = errors.New("bcn: invalid channel")
	// ErrFormat reports an unknown block format.
	ErrFormat = errors.New("bcn: unknown block format")
	// ErrPayload reports a payload length that does not match the size.
	ErrPayload = errors.New("bcn: payload length does not match the surface")
)

// Format names one block-compressed layout.
type Format int

// The formats this package writes.
const (
	// FormatUnknown is the invalid zero value.
	FormatUnknown Format = iota
	// FormatBC1RGB stores three colour channels and no alpha. The encoder
	// never emits the transparent index, so the payload decodes the same
	// under VK_FORMAT_BC1_RGB_UNORM_BLOCK and the RGBA variant.
	FormatBC1RGB
	// FormatBC1RGBA stores three colour channels and one alpha bit.
	FormatBC1RGBA
	// FormatBC3 stores three colour channels and eight alpha bits.
	FormatBC3
	// FormatBC4 stores one channel with eight bits.
	FormatBC4
	// FormatBC5 stores two channels with eight bits each.
	FormatBC5
)

// String names the format for a manifest or a metric.
func (f Format) String() string {
	switch f {
	case FormatBC1RGB:
		return "bc1-rgb"
	case FormatBC1RGBA:
		return "bc1-rgba"
	case FormatBC3:
		return "bc3"
	case FormatBC4:
		return "bc4"
	case FormatBC5:
		return "bc5"
	}
	return "unknown"
}

// BlockBytes returns the payload size of one 4x4 block. It returns zero for an
// unknown format.
func (f Format) BlockBytes() int {
	switch f {
	case FormatBC1RGB, FormatBC1RGBA, FormatBC4:
		return 8
	case FormatBC3, FormatBC5:
		return 16
	}
	return 0
}

// Channels returns how many source channels the format stores.
func (f Format) Channels() int {
	switch f {
	case FormatBC1RGB:
		return 3
	case FormatBC1RGBA, FormatBC3:
		return 4
	case FormatBC4:
		return 1
	case FormatBC5:
		return 2
	}
	return 0
}

// Channel indexes one component of a Surface. The values match
// texture.Channel, so a caller can convert between the two types.
type Channel int

// Channel indexes.
const (
	ChannelR Channel = iota
	ChannelG
	ChannelB
	ChannelA
)

// Transfer names the function the encoder applies to the colour channels
// before it quantizes them to eight bits.
//
// The zero value is illegal. Read the package doc for the reason.
type Transfer int

// The transfer functions.
const (
	// TransferUnspecified is the illegal zero value.
	TransferUnspecified Transfer = iota
	// TransferSRGB applies the IEC 61966-2-1 transfer function. Pick it for
	// a colour target that the GPU reads through an sRGB VkFormat.
	TransferSRGB
	// TransferUnorm stores the linear value directly. Pick it for a data
	// target such as a normal, roughness, metalness or occlusion map.
	TransferUnorm
)

// String names the transfer function for a manifest or a metric.
func (t Transfer) String() string {
	switch t {
	case TransferSRGB:
		return "srgb"
	case TransferUnorm:
		return "unorm"
	}
	return "unspecified"
}

// Quality trades encode time for image quality.
type Quality int

// The quality levels.
const (
	// QualityDefault means QualityHigh. An asset pipeline runs once and the
	// texture ships forever, so the default buys quality.
	QualityDefault Quality = iota
	// QualityFast fits a per-channel bounding box and refines it once.
	QualityFast
	// QualityHigh adds principal-axis initialization, more refinement
	// rounds, and a cluster fit over the sorted texel order.
	QualityHigh
)

// String names the quality level for a metric.
func (q Quality) String() string {
	if q == QualityFast {
		return "fast"
	}
	return "high"
}

// high reports whether the level runs the expensive search.
func (q Quality) high() bool { return q != QualityFast }

// BlocksAcross returns how many 4x4 blocks cover one edge. A mip level of one
// texel still costs one whole block.
func BlocksAcross(size int) int {
	if size < 1 {
		return 0
	}
	return (size + 3) / 4
}

// PayloadSize returns the payload length one mip level needs.
func PayloadSize(f Format, width, height int) int {
	block := f.BlockBytes()
	if block == 0 || width < 1 || height < 1 {
		return 0
	}
	return BlocksAcross(width) * BlocksAcross(height) * block
}

// MipChainBytes returns the GPU bytes a full mip chain costs, from the base
// size down to one texel by one texel.
//
// The count includes the padding every level below 4x4 carries, because the GPU
// allocates a whole block for a 2x2 or a 1x1 level. That padding is why a small
// texture saves less than the block ratio suggests.
func MipChainBytes(f Format, width, height int) int {
	if f.BlockBytes() == 0 || width < 1 || height < 1 {
		return 0
	}
	total := 0
	for {
		total += PayloadSize(f, width, height)
		if width <= 1 && height <= 1 {
			return total
		}
		width, height = halve(width), halve(height)
	}
}

// RGBA8MipChainBytes returns the GPU bytes the same chain costs as
// rgba8unorm. It is the denominator of the compression ratio.
func RGBA8MipChainBytes(width, height int) int {
	if width < 1 || height < 1 {
		return 0
	}
	total := 0
	for {
		total += width * height * 4
		if width <= 1 && height <= 1 {
			return total
		}
		width, height = halve(width), halve(height)
	}
}

func halve(v int) int {
	if v <= 1 {
		return 1
	}
	return v / 2
}
