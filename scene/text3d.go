package scene

import (
	"fmt"
	"html"
	"math"
	"strings"
	"unicode/utf8"
)

// Text3D draws plain text on a world-space plane through GoSX's existing HTML
// texture renderer. Width and Height are scene units; texture dimensions and
// Font use CSS pixels. Font faces come from the page's managed font styles.
// The plane starts in the XZ plane, matching HTML texture surfaces.
type Text3D struct {
	ID               string
	Target           string
	Text             string
	Position         Vector3
	Rotation         Euler
	Width            float64
	Height           float64
	Font             string
	Color            string
	Align            string
	LineHeight       float64
	TextureWidth     int
	TextureHeight    int
	MaxTexturePixels int
}

func (Text3D) sceneNode() {}

// Validate rejects invalid dimensions and unbounded text or texture allocations.
// Zero dimensions select defaults: a 2 by 0.5 plane and a 512 by 128 texture.
func (t Text3D) Validate() error {
	if !utf8.ValidString(t.Text) || utf8.RuneCountInString(t.Text) > 4096 {
		return fmt.Errorf("scene Text3D needs valid text of at most 4096 characters")
	}
	for _, v := range []float64{t.Width, t.Height, t.LineHeight, t.Position.X, t.Position.Y, t.Position.Z, t.Rotation.X, t.Rotation.Y, t.Rotation.Z} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("scene Text3D values must be finite")
		}
	}
	if t.Width < 0 || t.Height < 0 || t.LineHeight < 0 || t.LineHeight > 8 || t.TextureWidth < 0 || t.TextureWidth > 2048 || t.TextureHeight < 0 || t.TextureHeight > 2048 || t.MaxTexturePixels < 0 || t.MaxTexturePixels > 4*1024*1024 {
		return fmt.Errorf("scene Text3D dimensions are outside their bounds")
	}
	if t.Align != "" && t.Align != "left" && t.Align != "center" && t.Align != "right" {
		return fmt.Errorf("scene Text3D alignment must be left, center, or right")
	}
	tw, th, cap := t.TextureWidth, t.TextureHeight, t.MaxTexturePixels
	if tw == 0 {
		tw = 512
	}
	if th == 0 {
		th = 128
	}
	if cap == 0 {
		cap = 256 * 1024
	}
	if tw*th > cap {
		return fmt.Errorf("scene Text3D texture exceeds MaxTexturePixels")
	}
	// These values enter a CSS declaration, so reject declaration delimiters
	// while allowing ordinary quoted font families and CSS color functions.
	if len(t.Font) > 512 || len(t.Color) > 128 || strings.ContainsAny(t.Font+t.Color, ";{}<>\r\n") {
		return fmt.Errorf("scene Text3D font and color must be single CSS values")
	}
	return nil
}

// Surface returns the underlying texture surface for hosts that need its IR or
// an explicit validation error. NewGraph accepts Text3D directly.
func (t Text3D) Surface() (HTML, error) {
	if err := t.Validate(); err != nil {
		return HTML{}, err
	}
	width, height := t.Width, t.Height
	if width == 0 {
		width = 2
	}
	if height == 0 {
		height = .5
	}
	tw, th, maxPixels := t.TextureWidth, t.TextureHeight, t.MaxTexturePixels
	if tw == 0 {
		tw = 512
	}
	if th == 0 {
		th = 128
	}
	if maxPixels == 0 {
		maxPixels = 256 * 1024
	}
	font, color, align, lineHeight := t.Font, t.Color, t.Align, t.LineHeight
	if font == "" {
		font = "48px sans-serif"
	}
	if color == "" {
		color = "#ffffff"
	}
	if align == "" {
		align = "center"
	}
	if lineHeight == 0 {
		lineHeight = 1.2
	}
	style := fmt.Sprintf("box-sizing:border-box;width:%dpx;height:%dpx;display:flex;align-items:center;justify-content:%s;overflow:hidden;pointer-events:none", tw, th, map[string]string{"left": "flex-start", "center": "center", "right": "flex-end"}[align])
	textStyle := fmt.Sprintf("margin:0;max-width:100%%;white-space:pre-wrap;overflow-wrap:anywhere;font:%s;color:%s;line-height:%g;text-align:%s", font, color, lineHeight, align)
	markup := `<div role="img" aria-label="` + html.EscapeString(t.Text) + `" style="` + html.EscapeString(style) + `"><div style="` + html.EscapeString(textStyle) + `">` + html.EscapeString(t.Text) + `</div></div>`
	return HTML{ID: t.ID, Target: t.Target, Mode: HTMLTexture, Markup: markup,
		Position: t.Position, Rotation: t.Rotation, SurfaceWidth: width, SurfaceHeight: height,
		TextureWidth: tw, TextureHeight: th, MaxTexturePixels: maxPixels,
		Width: float64(tw), Height: float64(th), Opacity: 1, PointerEvents: "none"}, nil
}
