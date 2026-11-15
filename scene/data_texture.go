package scene

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// Host-written data textures.
//
// A Selena material declares a texture param (for example
// `param spectrum : texture2d format r16float`) and reads it with load().
// A Go/WASM engine owns the texels. It creates a texture under a key, writes
// rectangles of texels into it, and points the material at it with
// DataTextureRef(key) as the texture param's uniform value.
//
// Texels travel as the binary GSXT frame below, not as JSON, the same way
// instance transforms travel as GSXI frames (instance_stream.go). The browser
// runtime keeps one CPU copy per key and uploads only the changed rectangle to
// WebGPU (queue.writeTexture) or WebGL2 (texSubImage2D).

// TextureFormat is the storage format of a data texture. The numeric values
// are wire-stable. String returns the spelling Selena uses in
// `param t : texture2d format <name>` and in descriptor textures[].format.
type TextureFormat uint8

const (
	// TextureRGBA8Unorm is 4 bytes per texel, the Selena default.
	TextureRGBA8Unorm TextureFormat = iota
	// TextureRGBA16Float is 4 IEEE binary16 values per texel (8 bytes).
	TextureRGBA16Float
	// TextureRGBA32Float is 4 IEEE binary32 values per texel (16 bytes).
	TextureRGBA32Float
	// TextureR16Float is one IEEE binary16 value per texel (2 bytes).
	TextureR16Float
	// TextureR32Float is one IEEE binary32 value per texel (4 bytes).
	TextureR32Float
)

const (
	// MaxDataTextureSize is the largest width or height of a data texture. It
	// matches the WebGPU default maxTextureDimension2D and is far above the
	// WebGL2 minimum; a device with a smaller limit reports the texture as
	// unsupported at upload time.
	MaxDataTextureSize = 8192
	// MaxDataTextureBytes bounds one texture's storage. The browser keeps a CPU
	// copy of every texture, so this also bounds that copy.
	MaxDataTextureBytes = 256 << 20
	// MaxDataTextureKeyBytes bounds the key length.
	MaxDataTextureKeyBytes = 256
	// DataTextureRefPrefix starts a material uniform value that names a data
	// texture: "gosx:data:<key>".
	DataTextureRefPrefix = "gosx:data:"
)

var textureFormatNames = [...]string{"rgba8unorm", "rgba16float", "rgba32float", "r16float", "r32float"}
var textureFormatBytes = [...]int{4, 8, 16, 2, 4}

// String returns the Selena spelling of the format.
func (f TextureFormat) String() string {
	if int(f) < len(textureFormatNames) {
		return textureFormatNames[f]
	}
	return fmt.Sprintf("texture-format(%d)", uint8(f))
}

// Valid reports whether f is one of the five supported formats.
func (f TextureFormat) Valid() bool { return int(f) < len(textureFormatNames) }

// BytesPerTexel returns the size of one texel, or 0 for an invalid format.
func (f TextureFormat) BytesPerTexel() int {
	if f.Valid() {
		return textureFormatBytes[f]
	}
	return 0
}

// ParseTextureFormat reads a Selena format name such as "r16float".
func ParseTextureFormat(name string) (TextureFormat, error) {
	for i, candidate := range textureFormatNames {
		if candidate == name {
			return TextureFormat(i), nil
		}
	}
	return 0, fmt.Errorf("scene: unknown texture format %q (want one of %s)", name, strings.Join(textureFormatNames[:], ", "))
}

// DataTextureRef returns the material uniform value that binds a Selena
// texture param to the data texture under key.
func DataTextureRef(key string) string { return DataTextureRefPrefix + key }

// ValidateDataTextureKey reports why key cannot name a data texture.
func ValidateDataTextureKey(key string) error {
	switch {
	case key == "":
		return errors.New("scene: data texture key must not be empty")
	case len(key) > MaxDataTextureKeyBytes:
		return fmt.Errorf("scene: data texture key is %d bytes, the limit is %d", len(key), MaxDataTextureKeyBytes)
	case !utf8.ValidString(key):
		return errors.New("scene: data texture key must be valid UTF-8")
	}
	for _, r := range key {
		if r <= ' ' || r == 0x7f {
			return fmt.Errorf("scene: data texture key %q must not contain whitespace or control characters", key)
		}
	}
	return nil
}

// DataTextureOp selects what a DataTextureFrame does. The values are wire-stable.
type DataTextureOp uint8

const (
	// DataTextureWrite copies a rectangle of texels into an existing texture.
	// If the browser holds no texture under the key, it creates a zero-filled
	// one of the frame's size first.
	DataTextureWrite DataTextureOp = iota
	// DataTextureCreate makes a zero-filled texture of the frame's size and
	// format. An existing texture of the same size and format is cleared; one
	// with another size or format is replaced.
	DataTextureCreate
	// DataTextureDelete releases the texture and its GPU copies.
	DataTextureDelete
)

const (
	dataTextureMagic       = "GSXT"
	dataTextureVersion     = 1
	dataTextureHeaderBytes = 44 // magic(4) version(1) op(1) format(1) reserved(1) revision(8) width(4) height(4) originX(4) originY(4) rectW(4) rectH(4) keyLen(2) reserved(2)
)

// DataTextureFrame is one binary update for a data texture. Encode produces
// the bytes the browser's data-texture chunk decodes.
//
// Width, Height and Format describe the whole texture on every frame, so a
// frame is self-describing and the browser can reject a frame that disagrees
// with the texture it holds. The rectangle (OriginX, OriginY, RectWidth,
// RectHeight) is the part Texels replaces; Texels holds RectWidth*RectHeight
// texels in row-major order, top row first, with texel (0,0) at the top left,
// which is the texel load(tex, 0, 0) reads.
type DataTextureFrame struct {
	Key string
	// Revision must be positive and strictly increase per Key. The browser drops
	// a frame whose revision is not newer than the last one it applied.
	Revision uint64
	Op       DataTextureOp
	Format   TextureFormat
	// Width and Height are the texture's size in texels.
	Width, Height int
	// OriginX, OriginY, RectWidth and RectHeight give the rectangle written.
	// They are zero for DataTextureCreate and DataTextureDelete.
	OriginX, OriginY, RectWidth, RectHeight int
	// Texels holds the rectangle's bytes in Format's layout, little-endian.
	Texels []byte
}

// Validate reports the first reason f cannot be sent.
func (f DataTextureFrame) Validate() error {
	if f.Revision == 0 {
		return errors.New("scene: data texture revision must be positive")
	}
	if err := ValidateDataTextureKey(f.Key); err != nil {
		return err
	}
	if f.Op > DataTextureDelete {
		return fmt.Errorf("scene: unknown data texture op %d", uint8(f.Op))
	}
	if f.Op == DataTextureDelete {
		if f.Width != 0 || f.Height != 0 || f.RectWidth != 0 || f.RectHeight != 0 || f.OriginX != 0 || f.OriginY != 0 || len(f.Texels) != 0 || f.Format != 0 {
			return errors.New("scene: a data texture delete carries no size, format, rectangle or texels")
		}
		return nil
	}
	if err := validateDataTextureShape(f.Key, f.Width, f.Height, f.Format); err != nil {
		return err
	}
	if f.Op == DataTextureCreate {
		if f.RectWidth != 0 || f.RectHeight != 0 || f.OriginX != 0 || f.OriginY != 0 || len(f.Texels) != 0 {
			return errors.New("scene: a data texture create carries no rectangle or texels")
		}
		return nil
	}
	return validateDataTextureRect(f.Key, f.Width, f.Height, f.Format, f.OriginX, f.OriginY, f.RectWidth, f.RectHeight, len(f.Texels))
}

func validateDataTextureShape(key string, width, height int, format TextureFormat) error {
	if !format.Valid() {
		return fmt.Errorf("scene: data texture %q has unknown format %d", key, uint8(format))
	}
	if width < 1 || height < 1 || width > MaxDataTextureSize || height > MaxDataTextureSize {
		return fmt.Errorf("scene: data texture %q size %dx%d is outside 1..%d", key, width, height, MaxDataTextureSize)
	}
	if int64(width)*int64(height)*int64(format.BytesPerTexel()) > MaxDataTextureBytes {
		return fmt.Errorf("scene: data texture %q (%dx%d %s) exceeds %d bytes", key, width, height, format, MaxDataTextureBytes)
	}
	return nil
}

func validateDataTextureRect(key string, width, height int, format TextureFormat, x, y, w, h, byteLen int) error {
	if w < 1 || h < 1 {
		return fmt.Errorf("scene: data texture %q write rectangle %dx%d must be at least 1x1", key, w, h)
	}
	if x < 0 || y < 0 || x > width-w || y > height-h {
		return fmt.Errorf("scene: data texture %q write rectangle %dx%d at (%d,%d) falls outside the %dx%d texture", key, w, h, x, y, width, height)
	}
	if want := w * h * format.BytesPerTexel(); byteLen != want {
		return fmt.Errorf("scene: data texture %q write of %dx%d %s needs %d bytes, got %d", key, w, h, format, want, byteLen)
	}
	return nil
}

// Encode validates f and serializes it:
//
//	offset  size  field
//	0       4     magic "GSXT"
//	4       1     version (1)
//	5       1     op
//	6       1     format
//	7       1     reserved (0)
//	8       8     revision, uint64 LE
//	16      4     width, uint32 LE
//	20      4     height
//	24      4     originX
//	28      4     originY
//	32      4     rectWidth
//	36      4     rectHeight
//	40      2     keyLen, uint16 LE (unpadded)
//	42      2     reserved (0)
//	44      keyLen key bytes (UTF-8)
//	...     pad   zero bytes up to the next 4-byte boundary
//	pad     N     rectWidth*rectHeight texels
func (f DataTextureFrame) Encode() ([]byte, error) { return f.EncodeInto(nil) }

// EncodeInto writes the frame into reusable storage. The result aliases dst
// when its capacity is sufficient and stays valid until the caller reuses dst.
func (f DataTextureFrame) EncodeInto(dst []byte) ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	payload := align4(dataTextureHeaderBytes + len(f.Key))
	total := payload + len(f.Texels)
	if cap(dst) < total {
		dst = make([]byte, total)
	} else {
		dst = dst[:total]
		clear(dst[:payload])
	}
	copy(dst, dataTextureMagic)
	dst[4], dst[5], dst[6] = dataTextureVersion, byte(f.Op), byte(f.Format)
	binary.LittleEndian.PutUint64(dst[8:], f.Revision)
	for i, v := range [...]int{f.Width, f.Height, f.OriginX, f.OriginY, f.RectWidth, f.RectHeight} {
		binary.LittleEndian.PutUint32(dst[16+4*i:], uint32(v))
	}
	binary.LittleEndian.PutUint16(dst[40:], uint16(len(f.Key)))
	copy(dst[dataTextureHeaderBytes:], f.Key)
	copy(dst[payload:], f.Texels)
	return dst, nil
}

// DecodeDataTextureFrame parses and validates a GSXT frame. Texels aliases b.
func DecodeDataTextureFrame(b []byte) (DataTextureFrame, error) {
	if len(b) < dataTextureHeaderBytes || string(b[:4]) != dataTextureMagic {
		return DataTextureFrame{}, errors.New("scene: not a data texture frame")
	}
	if b[4] != dataTextureVersion {
		return DataTextureFrame{}, fmt.Errorf("scene: unsupported data texture frame version %d", b[4])
	}
	keyLen := int(binary.LittleEndian.Uint16(b[40:]))
	payload := align4(dataTextureHeaderBytes + keyLen)
	if len(b) < payload {
		return DataTextureFrame{}, errors.New("scene: data texture frame is shorter than its key")
	}
	u32 := func(i int) int { return int(binary.LittleEndian.Uint32(b[16+4*i:])) }
	f := DataTextureFrame{
		Key:      string(b[dataTextureHeaderBytes : dataTextureHeaderBytes+keyLen]),
		Revision: binary.LittleEndian.Uint64(b[8:]),
		Op:       DataTextureOp(b[5]),
		Format:   TextureFormat(b[6]),
		Width:    u32(0), Height: u32(1), OriginX: u32(2), OriginY: u32(3), RectWidth: u32(4), RectHeight: u32(5),
		Texels: b[payload:],
	}
	return f, f.Validate()
}

// DataTextureEncoder tracks the textures one producer has declared and turns
// calls into validated frames with increasing revisions. It does no I/O; the
// js/wasm TextureWriter sends its frames to the renderer. Use it directly to
// test a producer natively or to carry frames over another transport.
// An encoder is not safe for concurrent use.
type DataTextureEncoder struct {
	textures map[string]dataTextureState
	buf      []byte
}

type dataTextureState struct {
	width, height int
	format        TextureFormat
	revision      uint64 // survives Delete, so a reused key keeps rising
	live          bool
}

// NewDataTextureEncoder returns an empty encoder.
func NewDataTextureEncoder() *DataTextureEncoder {
	return &DataTextureEncoder{textures: map[string]dataTextureState{}}
}

// Create declares a width x height texture in format under key and returns the
// frame that creates it. Creating a key again clears it, and replaces it when
// the size or format differs. The returned bytes are valid until the next call.
func (e *DataTextureEncoder) Create(key string, width, height int, format TextureFormat) ([]byte, error) {
	if err := validateDataTextureShape(key, width, height, format); err != nil {
		return nil, err
	}
	if err := ValidateDataTextureKey(key); err != nil {
		return nil, err
	}
	state := e.textures[key]
	state.width, state.height, state.format, state.live = width, height, format, true
	state.revision++
	frame, err := DataTextureFrame{Key: key, Revision: state.revision, Op: DataTextureCreate, Format: format, Width: width, Height: height}.EncodeInto(e.buf)
	if err != nil {
		return nil, err
	}
	e.textures[key], e.buf = state, frame
	return frame, nil
}

// WriteTexture returns the frame that copies texels into the width x height
// rectangle whose top-left texel is (originX, originY) of the texture created
// under key. format must equal the format passed to Create. texels holds
// width*height texels in that format, little-endian, top row first. The
// returned bytes are valid until the next call.
func (e *DataTextureEncoder) WriteTexture(key string, width, height int, format TextureFormat, texels []byte, originX, originY int) ([]byte, error) {
	state := e.textures[key]
	if !state.live {
		return nil, fmt.Errorf("scene: data texture %q was not created; call Create first", key)
	}
	if format != state.format {
		return nil, fmt.Errorf("scene: data texture %q is %s, not %s", key, state.format, format)
	}
	state.revision++
	frame, err := DataTextureFrame{
		Key: key, Revision: state.revision, Op: DataTextureWrite, Format: format, Width: state.width, Height: state.height,
		OriginX: originX, OriginY: originY, RectWidth: width, RectHeight: height, Texels: texels,
	}.EncodeInto(e.buf)
	if err != nil {
		return nil, err
	}
	e.textures[key], e.buf = state, frame
	return frame, nil
}

// Delete returns the frame that releases the texture under key and forgets it.
func (e *DataTextureEncoder) Delete(key string) ([]byte, error) {
	state := e.textures[key]
	if !state.live {
		return nil, fmt.Errorf("scene: data texture %q was not created", key)
	}
	state.revision++
	state.live = false
	frame, err := DataTextureFrame{Key: key, Revision: state.revision, Op: DataTextureDelete}.EncodeInto(e.buf)
	if err != nil {
		return nil, err
	}
	e.textures[key], e.buf = state, frame
	return frame, nil
}

// Float16Bits converts f to IEEE binary16 bits, rounding to nearest even.
// Values beyond the binary16 range become infinity; NaN stays NaN.
func Float16Bits(f float32) uint16 {
	b := math.Float32bits(f)
	sign := uint16(b>>16) & 0x8000
	exp := int32(b>>23) & 0xff
	frac := b & 0x7fffff
	switch {
	case exp == 0xff: // infinity or NaN
		if frac != 0 {
			return sign | 0x7e00
		}
		return sign | 0x7c00
	case exp > 142: // exponent above 15 after rebias
		return sign | 0x7c00
	case exp < 102: // below a quarter of the smallest subnormal
		return sign
	case exp < 113: // subnormal result
		frac |= 0x800000
		shift := uint(126 - exp)
		half := uint16(frac >> shift)
		rem := frac & (1<<shift - 1)
		mid := uint32(1) << (shift - 1)
		if rem > mid || rem == mid && half&1 == 1 {
			half++
		}
		return sign | half
	}
	half := uint16(exp-112)<<10 | uint16(frac>>13)
	rem := frac & 0x1fff
	if rem > 0x1000 || rem == 0x1000 && half&1 == 1 {
		half++ // a carry into the exponent is correct, including up to infinity
	}
	return sign | half
}

// AppendFloat16 appends src as little-endian binary16 texels (R16Float, or
// four per texel for RGBA16Float) and returns the extended slice.
func AppendFloat16(dst []byte, src []float32) []byte {
	for _, v := range src {
		dst = binary.LittleEndian.AppendUint16(dst, Float16Bits(v))
	}
	return dst
}

// AppendFloat32 appends src as little-endian binary32 texels (R32Float, or four
// per texel for RGBA32Float) and returns the extended slice.
func AppendFloat32(dst []byte, src []float32) []byte {
	for _, v := range src {
		dst = binary.LittleEndian.AppendUint32(dst, math.Float32bits(v))
	}
	return dst
}
