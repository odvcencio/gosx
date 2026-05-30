//go:build js && wasm

package surface

// noopImpl is a no-op canvasImpl for use in the WASM fallback path (e.g. when
// wrap.go type-assertions miss because the caller passed a json.RawMessage
// instead of a typed payload). In practice this should never be reached at
// runtime, but it ensures the package compiles cleanly in the WASM target.
type noopImpl struct{}

func (n *noopImpl) width() int                             { return 0 }
func (n *noopImpl) height() int                            { return 0 }
func (n *noopImpl) clear()                                 {}
func (n *noopImpl) clearRect(x, y, w, h float64)           {}
func (n *noopImpl) fillRect(x, y, w, h float64)            {}
func (n *noopImpl) beginPath()                             {}
func (n *noopImpl) moveTo(x, y float64)                    {}
func (n *noopImpl) lineTo(x, y float64)                    {}
func (n *noopImpl) arc(x, y, r, start, end float64)        {}
func (n *noopImpl) stroke()                                {}
func (n *noopImpl) fill()                                  {}
func (n *noopImpl) fillText(text string, x, y float64)     {}
func (n *noopImpl) setFillStyle(css string)                {}
func (n *noopImpl) setStrokeStyle(css string)              {}
func (n *noopImpl) setLineWidth(w float64)                 {}
func (n *noopImpl) setFont(css string)                     {}
func (n *noopImpl) setTextAlign(align string)              {}
func (n *noopImpl) save()                                  {}
func (n *noopImpl) restore()                               {}
func (n *noopImpl) translate(x, y float64)                 {}
func (n *noopImpl) scale(x, y float64)                     {}
func (n *noopImpl) rotate(rad float64)                     {}
func (n *noopImpl) setTransform(a, b, c2, d, e, f float64) {}
func (n *noopImpl) requestFrame()                          {}
func (n *noopImpl) startLoop(step func(dt float64))        {}

// newNoopCanvas returns a Canvas backed by a no-op implementation.
// Used as a fallback when no real canvas context is available.
func newNoopCanvas() *Canvas {
	return &Canvas{impl: &noopImpl{}}
}
