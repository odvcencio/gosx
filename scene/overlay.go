package scene

import (
	"fmt"
	"math"
	"strings"

	"m31labs.dev/gosx"
)

// Overlay places GoSX content above the scene at a projected world position.
// With Target set, Position is local to that object's current transform,
// including its rotation, scale, and animation. Without Target it is a world
// position, or a local position when the overlay belongs to a Group.
// Width and Height are world units; OffsetX and OffsetY are CSS pixels.
// Content uses ordinary HTML and CSS outside the scene's post-processing.
type Overlay struct {
	ID, Target, ClassName string
	Content               gosx.Node
	Position              Vector3
	Width, Height         float64
	OffsetX, OffsetY      float64
	Priority              float64
	Occlude               bool
	Interactive           bool
}

func (Overlay) sceneNode() {}

const overlayPlacementStyle = "position:absolute;box-sizing:border-box;left:var(--gosx-scene-html-left);top:var(--gosx-scene-html-top);width:var(--gosx-scene-html-width);min-height:var(--gosx-scene-html-min-height);transform:translate(-50%,-50%);opacity:var(--gosx-scene-html-opacity);z-index:var(--gosx-scene-html-z-index);pointer-events:var(--gosx-scene-html-pointer-events)"

func (o Overlay) markup() string {
	return gosx.RenderHTML(gosx.El("div", gosx.Attrs(gosx.Attr("style", overlayPlacementStyle), gosx.Attr("class", o.ClassName)), o.Content))
}

// Validate checks placement and the bounded HTML payload.
func (o Overlay) Validate() error {
	for _, n := range []float64{o.Position.X, o.Position.Y, o.Position.Z, o.Width, o.Height, o.OffsetX, o.OffsetY, o.Priority} {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return fmt.Errorf("scene: overlay placement must be finite")
		}
	}
	if o.Width < 0 || o.Height < 0 {
		return fmt.Errorf("scene: overlay dimensions must be nonnegative")
	}
	if len(o.ID) > 256 || len(o.Target) > 256 || len(o.ClassName) > 512 {
		return fmt.Errorf("scene: overlay identifiers or class exceed their limits")
	}
	markup := gosx.RenderHTML(o.Content)
	if strings.TrimSpace(markup) == "" || len(o.markup()) > 64*1024 {
		return fmt.Errorf("scene: overlay content must contain 1 to 65536 bytes of HTML")
	}
	return nil
}

// Surface returns the existing DOM overlay representation. Invalid graph nodes
// are omitted; call Validate or Surface when errors should be returned to callers.
func (o Overlay) Surface() (HTML, error) {
	if err := o.Validate(); err != nil {
		return HTML{}, err
	}
	width, height := o.Width, o.Height
	if width == 0 {
		width = 2
	}
	if height == 0 {
		height = .72
	}
	pointerEvents := "none"
	if o.Interactive {
		pointerEvents = "auto"
	}
	return HTML{ID: o.ID, Target: o.Target, FollowTarget: true, Mode: HTMLDOM,
		Markup: o.markup(), Position: o.Position,
		Width: width, Height: height, OffsetX: o.OffsetX, OffsetY: o.OffsetY,
		Priority: o.Priority, Occlude: o.Occlude, PointerEvents: pointerEvents}, nil
}
