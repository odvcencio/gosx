package scene

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
)

// TextAtlas is one texture of short text labels, such as jersey numbers,
// nameplates or unit tags, drawn in the browser with the canvas 2D API when
// the scene mounts. Every label gets one cell of a grid, so one texture and
// one material serve every player: a material picks its player's cell with
// Region instead of loading a texture per player.
//
// Use Texture as the texture URL of a material, then select the cell:
//
//   - Selena: pass Texture as the texture2d param and Region(i) as a vec4
//     param (the materiallib game module's mlAtlasUV maps into it; see
//     Selena's examples/material-library/jersey.sel).
//   - Standard materials: set Texture as the material's Texture and draw the
//     label on Quad(i, w, h), whose UVs cover the cell.
//
// An empty label leaves its cell transparent, for a blank nameplate. List the
// atlas in Props.TextAtlases; a changed label set gets a new Texture URL, so
// the renderer uploads the new image.
type TextAtlas struct {
	// ID names the atlas; it must be unique within the scene.
	ID string
	// Labels holds one string per cell, in order. "" is a blank cell.
	Labels []string
	// CellWidth and CellHeight are the cell size in pixels. Zero means 128
	// by 64.
	CellWidth, CellHeight int
	// Columns is the number of cells per row. Zero fits as many as a
	// 4096-pixel row holds, up to the label count.
	Columns int
	// Font is a CSS font family. Empty means "system-ui, sans-serif".
	Font string
	// Weight is a CSS font weight. Empty means "700".
	Weight string
	// Color is the text color. Empty means "#ffffff".
	Color string
	// Background fills each non-blank cell except a transparent margin of
	// up to 4 pixels, which keeps texture filtering from bleeding the fill
	// into the next cell. Empty means transparent.
	Background string
	// OutlineColor and OutlineWidth (pixels) draw a stroke around the text.
	OutlineColor string
	OutlineWidth float64
	// Padding is the share of the cell kept clear on each side, 0..0.45.
	// Zero means 0.08. Text shrinks to fit inside the padding.
	Padding float64
}

const (
	// TextAtlasScheme prefixes the texture URL of a text atlas.
	TextAtlasScheme       = "gosx-text-atlas:"
	textAtlasMaxSide      = 4096
	textAtlasDefaultCellW = 128
	textAtlasDefaultCellH = 64
)

func (a TextAtlas) normalized() TextAtlas {
	if a.CellWidth <= 0 {
		a.CellWidth = textAtlasDefaultCellW
	}
	if a.CellHeight <= 0 {
		a.CellHeight = textAtlasDefaultCellH
	}
	if a.Columns <= 0 {
		a.Columns = textAtlasMaxSide / a.CellWidth
		if a.Columns > len(a.Labels) {
			a.Columns = len(a.Labels)
		}
		if a.Columns < 1 {
			a.Columns = 1
		}
	}
	if strings.TrimSpace(a.Font) == "" {
		a.Font = "system-ui, sans-serif"
	}
	if strings.TrimSpace(a.Weight) == "" {
		a.Weight = "700"
	}
	if strings.TrimSpace(a.Color) == "" {
		a.Color = "#ffffff"
	}
	if a.Padding <= 0 || math.IsNaN(a.Padding) {
		a.Padding = 0.08
	}
	if a.Padding > 0.45 {
		a.Padding = 0.45
	}
	if a.OutlineWidth < 0 || math.IsNaN(a.OutlineWidth) {
		a.OutlineWidth = 0
	}
	return a
}

// Size returns the atlas texture size in pixels.
func (a TextAtlas) Size() (width, height int) {
	n := a.normalized()
	rows := (len(n.Labels) + n.Columns - 1) / n.Columns
	if rows < 1 {
		rows = 1
	}
	return n.Columns * n.CellWidth, rows * n.CellHeight
}

// Validate reports an atlas the browser cannot draw: no ID, an ID with
// characters outside [A-Za-z0-9_-], or a texture larger than 4096 pixels on a
// side.
func (a TextAtlas) Validate() error {
	if a.ID == "" {
		return errors.New("scene: text atlas needs an ID")
	}
	for _, r := range a.ID {
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return fmt.Errorf("scene: text atlas ID %q may use only letters, digits, - and _", a.ID)
		}
	}
	w, h := a.Size()
	if w > textAtlasMaxSide || h > textAtlasMaxSide {
		return fmt.Errorf("scene: text atlas %q is %dx%d pixels; the limit is %d per side (use smaller cells or another atlas)", a.ID, w, h, textAtlasMaxSide)
	}
	return nil
}

// Texture returns the URL that materials use for this atlas. It carries a
// hash of the labels and style, so a changed atlas is a new texture.
func (a TextAtlas) Texture() string {
	data, _ := json.Marshal(a.normalized().wire())
	h := fnv.New64a()
	_, _ = h.Write(data)
	return fmt.Sprintf("%s%s#%016x", TextAtlasScheme, a.ID, h.Sum64())
}

// Region returns cell i as (u0, v0, u1, v1) in the atlas texture's UV space,
// with (0, 0) at the image's top-left corner, as GoSX samples images. An
// index outside Labels returns a zero region.
func (a TextAtlas) Region(i int) [4]float64 {
	n := a.normalized()
	if i < 0 || i >= len(n.Labels) {
		return [4]float64{}
	}
	w, h := a.Size()
	col, row := i%n.Columns, i/n.Columns
	return [4]float64{
		float64(col*n.CellWidth) / float64(w),
		float64(row*n.CellHeight) / float64(h),
		float64((col+1)*n.CellWidth) / float64(w),
		float64((row+1)*n.CellHeight) / float64(h),
	}
}

// RegionUniform is Region as a []float64, the form a vec4 Selena param takes
// in SelenaMaterialOptions.Uniforms.
func (a TextAtlas) RegionUniform(i int) []float64 {
	r := a.Region(i)
	return r[:]
}

// Quad returns a width by height rectangle in the XY plane, facing +Z, whose
// UVs cover cell i, for drawing the label with a standard material.
func (a TextAtlas) Quad(i int, width, height float64) BufferGeometry {
	r := a.Region(i)
	x, y := width/2, height/2
	return BufferGeometry{
		Positions: []float64{-x, -y, 0, x, -y, 0, x, y, 0, -x, y, 0},
		Normals:   []float64{0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1},
		// Image rows run top to bottom, so the quad's top edge reads v0.
		UVs:     []float64{r[0], r[3], r[2], r[3], r[2], r[1], r[0], r[1]},
		Indices: []int{0, 1, 2, 0, 2, 3},
	}
}

// textAtlasWire is the browser's drawing spec for one atlas.
type textAtlasWire struct {
	ID           string   `json:"id"`
	Texture      string   `json:"texture,omitempty"`
	Labels       []string `json:"labels"`
	CellWidth    int      `json:"cellWidth"`
	CellHeight   int      `json:"cellHeight"`
	Columns      int      `json:"columns"`
	Font         string   `json:"font"`
	Weight       string   `json:"weight"`
	Color        string   `json:"color"`
	Background   string   `json:"background,omitempty"`
	OutlineColor string   `json:"outlineColor,omitempty"`
	OutlineWidth float64  `json:"outlineWidth,omitempty"`
	Padding      float64  `json:"padding"`
}

func (a TextAtlas) wire() textAtlasWire {
	labels := a.Labels
	if labels == nil {
		labels = []string{}
	}
	return textAtlasWire{ID: a.ID, Labels: labels, CellWidth: a.CellWidth, CellHeight: a.CellHeight,
		Columns: a.Columns, Font: a.Font, Weight: a.Weight, Color: a.Color, Background: a.Background,
		OutlineColor: a.OutlineColor, OutlineWidth: a.OutlineWidth, Padding: a.Padding}
}

// textAtlasProps lowers the valid atlases into props.textAtlases. An invalid
// atlas is dropped, since Props has no error path; Validate reports why.
func textAtlasProps(atlases []TextAtlas) []any {
	out := make([]any, 0, len(atlases))
	for _, a := range atlases {
		if a.Validate() != nil {
			continue
		}
		w := a.normalized().wire()
		w.Texture = a.Texture()
		out = append(out, w)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
