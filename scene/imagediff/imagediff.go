// Package imagediff compares two rendered frames and reports where they
// differ. It closes the last browser-shaped hole in the native authoring loop:
// a frame hash proves that something changed, but it cannot say what. This
// package localizes the change to pixel regions and writes a diff image, with
// no browser, no headless Chrome, and no pixelmatch dependency.
//
// Every number here comes from reading the two images. Nothing is assumed.
package imagediff

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"sort"
)

// Schema names the report shape. Machine consumers pin this value.
const Schema = "gosx.scene3d.imagediff/v1"

const (
	defaultTileSize   = 16
	defaultMaxRegions = 16
)

// Options controls one comparison.
type Options struct {
	// Tolerance is the largest per-channel absolute difference, in 0-255
	// units, that still counts as unchanged. Zero demands exact equality.
	Tolerance int
	// TileSize is the edge length in pixels of the grid used to group changed
	// pixels into regions. Zero uses 16.
	TileSize int
	// MaxRegions caps how many regions the report lists. Zero uses 16. The
	// report always states how many regions were found before the cap.
	MaxRegions int
}

// Region is an axis-aligned box that contains changed pixels. Coordinates are
// inclusive and are in image pixel space.
type Region struct {
	MinX            int `json:"minX"`
	MinY            int `json:"minY"`
	MaxX            int `json:"maxX"`
	MaxY            int `json:"maxY"`
	ChangedPixels   int `json:"changedPixels"`
	MaxChannelDelta int `json:"maxChannelDelta"`
}

// Width returns the inclusive box width in pixels.
func (r Region) Width() int { return r.MaxX - r.MinX + 1 }

// Height returns the inclusive box height in pixels.
func (r Region) Height() int { return r.MaxY - r.MinY + 1 }

// String renders the region as a stable, greppable location.
func (r Region) String() string {
	return fmt.Sprintf("x=%d..%d y=%d..%d (%dx%d) changed=%d maxDelta=%d",
		r.MinX, r.MaxX, r.MinY, r.MaxY, r.Width(), r.Height(), r.ChangedPixels, r.MaxChannelDelta)
}

// Result is the complete comparison evidence. Field order and slice order are
// stable, so an agent can compare two reports without re-deriving anything.
type Result struct {
	Schema string `json:"schema"`
	// Identical is true when every compared pixel is within Tolerance and the
	// two images have the same dimensions.
	Identical bool `json:"identical"`
	// SizeMismatch is true when the two images have different dimensions. The
	// comparison then covers only the overlapping rectangle.
	SizeMismatch     bool    `json:"sizeMismatch"`
	Tolerance        int     `json:"tolerance"`
	ReferenceWidth   int     `json:"referenceWidth"`
	ReferenceHeight  int     `json:"referenceHeight"`
	CandidateWidth   int     `json:"candidateWidth"`
	CandidateHeight  int     `json:"candidateHeight"`
	ComparedPixels   int     `json:"comparedPixels"`
	ChangedPixels    int     `json:"changedPixels"`
	ChangedFraction  float64 `json:"changedFraction"`
	MaxChannelDelta  int     `json:"maxChannelDelta"`
	MeanChannelDelta float64 `json:"meanChannelDelta"`
	// Bounds is the single box that contains every changed pixel. It is nil
	// when nothing changed.
	Bounds *Region `json:"bounds,omitempty"`
	// RegionCount is how many separate changed regions were found before
	// MaxRegions truncated the list.
	RegionCount int `json:"regionCount"`
	// Regions are the changed regions, largest first, then top-to-bottom and
	// left-to-right. The order is stable for a given pair of images.
	Regions []Region `json:"regions,omitempty"`
	// ReferenceSHA256 and CandidateSHA256 hash the raw RGBA pixels, not the
	// PNG container, so a re-encode does not look like a visual change.
	ReferenceSHA256 string `json:"referenceSHA256"`
	CandidateSHA256 string `json:"candidateSHA256"`
	// DiffSHA256 hashes the raw RGBA pixels of the diff image.
	DiffSHA256 string `json:"diffSHA256"`
	// Image is the diff visualization. Unchanged pixels keep a dimmed grey copy
	// of the reference. Changed pixels are red, and brighter red means a larger
	// difference. It is never nil after a successful compare.
	Image *image.RGBA `json:"-"`
}

// Compare reads both images and returns localized difference evidence.
func Compare(reference, candidate image.Image, opts Options) (Result, error) {
	if reference == nil || candidate == nil {
		return Result{}, errors.New("imagediff: both reference and candidate images are required")
	}
	opts = normalize(opts)
	ref := toRGBA(reference)
	cand := toRGBA(candidate)
	refBounds, candBounds := ref.Bounds(), cand.Bounds()

	result := Result{
		Schema:          Schema,
		Tolerance:       opts.Tolerance,
		ReferenceWidth:  refBounds.Dx(),
		ReferenceHeight: refBounds.Dy(),
		CandidateWidth:  candBounds.Dx(),
		CandidateHeight: candBounds.Dy(),
		ReferenceSHA256: pixelHash(ref),
		CandidateSHA256: pixelHash(cand),
		SizeMismatch:    refBounds.Dx() != candBounds.Dx() || refBounds.Dy() != candBounds.Dy(),
	}

	width := minInt(refBounds.Dx(), candBounds.Dx())
	height := minInt(refBounds.Dy(), candBounds.Dy())
	diff := image.NewRGBA(image.Rect(0, 0, maxInt(width, 1), maxInt(height, 1)))
	result.ComparedPixels = width * height

	tilesX := (width + opts.TileSize - 1) / opts.TileSize
	tilesY := (height + opts.TileSize - 1) / opts.TileSize
	tiles := make([]tileStat, maxInt(tilesX*tilesY, 0))

	var deltaSum float64
	bounds := Region{MinX: width, MinY: height, MaxX: -1, MaxY: -1}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			a := ref.RGBAAt(refBounds.Min.X+x, refBounds.Min.Y+y)
			b := cand.RGBAAt(candBounds.Min.X+x, candBounds.Min.Y+y)
			delta := channelDelta(a, b)
			deltaSum += float64(delta)
			if delta <= opts.Tolerance {
				diff.SetRGBA(x, y, dimmed(a))
				continue
			}
			result.ChangedPixels++
			if delta > result.MaxChannelDelta {
				result.MaxChannelDelta = delta
			}
			diff.SetRGBA(x, y, changedColor(delta))
			bounds.MinX = minInt(bounds.MinX, x)
			bounds.MinY = minInt(bounds.MinY, y)
			bounds.MaxX = maxInt(bounds.MaxX, x)
			bounds.MaxY = maxInt(bounds.MaxY, y)
			if tilesX > 0 {
				index := (y/opts.TileSize)*tilesX + x/opts.TileSize
				tile := &tiles[index]
				tile.changed++
				tile.minX = minOrSet(tile.minX, x, tile.changed == 1)
				tile.minY = minOrSet(tile.minY, y, tile.changed == 1)
				tile.maxX = maxInt(tile.maxX, x)
				tile.maxY = maxInt(tile.maxY, y)
				if delta > tile.maxDelta {
					tile.maxDelta = delta
				}
			}
		}
	}

	if result.ComparedPixels > 0 {
		result.ChangedFraction = float64(result.ChangedPixels) / float64(result.ComparedPixels)
		result.MeanChannelDelta = deltaSum / float64(result.ComparedPixels)
	}
	if result.ChangedPixels > 0 {
		bounds.ChangedPixels = result.ChangedPixels
		bounds.MaxChannelDelta = result.MaxChannelDelta
		result.Bounds = &bounds
		regions := clusterRegions(tiles, tilesX, tilesY)
		result.RegionCount = len(regions)
		if len(regions) > opts.MaxRegions {
			regions = regions[:opts.MaxRegions]
		}
		result.Regions = regions
	}
	result.Identical = result.ChangedPixels == 0 && !result.SizeMismatch
	result.Image = diff
	result.DiffSHA256 = pixelHash(diff)
	return result, nil
}

// ComparePNG decodes two PNG payloads and compares them.
func ComparePNG(reference, candidate []byte, opts Options) (Result, error) {
	ref, err := png.Decode(bytes.NewReader(reference))
	if err != nil {
		return Result{}, fmt.Errorf("imagediff: decode reference PNG: %w", err)
	}
	cand, err := png.Decode(bytes.NewReader(candidate))
	if err != nil {
		return Result{}, fmt.Errorf("imagediff: decode candidate PNG: %w", err)
	}
	return Compare(ref, cand, opts)
}

// WritePNG encodes the diff image.
func (r Result) WritePNG(w io.Writer) error {
	if r.Image == nil {
		return errors.New("imagediff: result has no diff image")
	}
	return png.Encode(w, r.Image)
}

// Summary returns one stable human line describing the comparison.
func (r Result) Summary() string {
	if r.SizeMismatch {
		return fmt.Sprintf("size mismatch: reference %dx%d, candidate %dx%d",
			r.ReferenceWidth, r.ReferenceHeight, r.CandidateWidth, r.CandidateHeight)
	}
	if r.Identical {
		return fmt.Sprintf("identical: %d pixels compared at tolerance %d", r.ComparedPixels, r.Tolerance)
	}
	return fmt.Sprintf("%d of %d pixels changed (%.4f%%), max channel delta %d, %d %s",
		r.ChangedPixels, r.ComparedPixels, r.ChangedFraction*100, r.MaxChannelDelta,
		r.RegionCount, plural(r.RegionCount, "region", "regions"))
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

type tileStat struct {
	changed  int
	minX     int
	minY     int
	maxX     int
	maxY     int
	maxDelta int
}

// clusterRegions merges touching changed tiles into regions. It walks tiles in
// row-major order and grows each region with a breadth-first search, so the
// same pair of images always produces the same regions in the same order.
func clusterRegions(tiles []tileStat, tilesX, tilesY int) []Region {
	if tilesX <= 0 || tilesY <= 0 {
		return nil
	}
	visited := make([]bool, len(tiles))
	var regions []Region
	queue := make([]int, 0, 64)
	for start := range tiles {
		if visited[start] || tiles[start].changed == 0 {
			continue
		}
		region := Region{MinX: tiles[start].minX, MinY: tiles[start].minY, MaxX: -1, MaxY: -1}
		visited[start] = true
		queue = append(queue[:0], start)
		for len(queue) > 0 {
			index := queue[0]
			queue = queue[1:]
			tile := tiles[index]
			region.ChangedPixels += tile.changed
			region.MinX = minInt(region.MinX, tile.minX)
			region.MinY = minInt(region.MinY, tile.minY)
			region.MaxX = maxInt(region.MaxX, tile.maxX)
			region.MaxY = maxInt(region.MaxY, tile.maxY)
			if tile.maxDelta > region.MaxChannelDelta {
				region.MaxChannelDelta = tile.maxDelta
			}
			tx, ty := index%tilesX, index/tilesX
			for _, step := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				nx, ny := tx+step[0], ty+step[1]
				if nx < 0 || ny < 0 || nx >= tilesX || ny >= tilesY {
					continue
				}
				next := ny*tilesX + nx
				if visited[next] || tiles[next].changed == 0 {
					continue
				}
				visited[next] = true
				queue = append(queue, next)
			}
		}
		regions = append(regions, region)
	}
	sort.SliceStable(regions, func(i, j int) bool {
		if regions[i].ChangedPixels != regions[j].ChangedPixels {
			return regions[i].ChangedPixels > regions[j].ChangedPixels
		}
		if regions[i].MinY != regions[j].MinY {
			return regions[i].MinY < regions[j].MinY
		}
		return regions[i].MinX < regions[j].MinX
	})
	return regions
}

func normalize(opts Options) Options {
	if opts.Tolerance < 0 {
		opts.Tolerance = 0
	}
	if opts.TileSize <= 0 {
		opts.TileSize = defaultTileSize
	}
	if opts.MaxRegions <= 0 {
		opts.MaxRegions = defaultMaxRegions
	}
	return opts
}

func channelDelta(a, b color.RGBA) int {
	delta := absInt(int(a.R) - int(b.R))
	delta = maxInt(delta, absInt(int(a.G)-int(b.G)))
	delta = maxInt(delta, absInt(int(a.B)-int(b.B)))
	delta = maxInt(delta, absInt(int(a.A)-int(b.A)))
	return delta
}

// dimmed converts a reference pixel to a faint grey backdrop. The backdrop
// keeps enough contrast to show where the change sits in the composition, and
// stays far enough below the red markers that the markers still dominate.
func dimmed(c color.RGBA) color.RGBA {
	luma := (int(c.R)*54 + int(c.G)*183 + int(c.B)*19) / 256
	grey := 18 + luma*55/100
	if grey > 140 {
		grey = 140
	}
	value := uint8(grey)
	return color.RGBA{R: value, G: value, B: value, A: 255}
}

// changedColor maps a per-channel delta onto a red marker. A one-level change
// is still clearly visible, and larger changes are brighter.
func changedColor(delta int) color.RGBA {
	value := 96 + delta*159/255
	if value > 255 {
		value = 255
	}
	return color.RGBA{R: uint8(value), A: 255}
}

func toRGBA(src image.Image) *image.RGBA {
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	bounds := src.Bounds()
	out := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			out.SetRGBA(x, y, color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)})
		}
	}
	return out
}

// pixelHash hashes the dimensions and the raw RGBA bytes. It ignores the PNG
// container, so re-encoding the same pixels keeps the same hash.
func pixelHash(img *image.RGBA) string {
	h := sha256.New()
	bounds := img.Bounds()
	fmt.Fprintf(h, "rgba/%d/%d\n", bounds.Dx(), bounds.Dy())
	stride := bounds.Dx() * 4
	for y := 0; y < bounds.Dy(); y++ {
		offset := img.PixOffset(bounds.Min.X, bounds.Min.Y+y)
		h.Write(img.Pix[offset : offset+stride])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func minOrSet(current, value int, first bool) int {
	if first {
		return value
	}
	return minInt(current, value)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
