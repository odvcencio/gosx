//go:build !(js && wasm)

package surface

// stubImpl is a no-op canvasImpl for host (non-WASM) builds.
// It satisfies the canvasImpl interface so that wrap.go fallback paths and
// tests compile and run without a browser.
type stubImpl struct {
	w, h int
}

func (s *stubImpl) width() int                             { return s.w }
func (s *stubImpl) height() int                            { return s.h }
func (s *stubImpl) clear()                                 {}
func (s *stubImpl) clearRect(x, y, w, h float64)           {}
func (s *stubImpl) fillRect(x, y, w, h float64)            {}
func (s *stubImpl) beginPath()                             {}
func (s *stubImpl) moveTo(x, y float64)                    {}
func (s *stubImpl) lineTo(x, y float64)                    {}
func (s *stubImpl) arc(x, y, r, start, end float64)        {}
func (s *stubImpl) stroke()                                {}
func (s *stubImpl) fill()                                  {}
func (s *stubImpl) fillText(text string, x, y float64)     {}
func (s *stubImpl) setFillStyle(css string)                {}
func (s *stubImpl) setStrokeStyle(css string)              {}
func (s *stubImpl) setLineWidth(w float64)                 {}
func (s *stubImpl) setFont(css string)                     {}
func (s *stubImpl) setTextAlign(align string)              {}
func (s *stubImpl) save()                                  {}
func (s *stubImpl) restore()                               {}
func (s *stubImpl) translate(x, y float64)                 {}
func (s *stubImpl) scale(x, y float64)                     {}
func (s *stubImpl) rotate(rad float64)                     {}
func (s *stubImpl) setTransform(a, b, c2, d, e, f float64) {}
func (s *stubImpl) requestFrame()                          {}
func (s *stubImpl) startLoop(step func(dt float64))        {}

// newNoopCanvas returns a Canvas backed by the no-op stub.
// Used as a fallback when no real canvas context is available.
func newNoopCanvas() *Canvas {
	return &Canvas{impl: &stubImpl{}}
}

// SplitKeyPayloadStr is a stub for host builds; the real version is in canvas_js.go.
func SplitKeyPayloadStr(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '\t' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

// DecodeBase64Props is a stub for host builds; the real version uses browser atob.
func DecodeBase64Props(b64 string) []byte {
	if b64 == "" {
		return nil
	}
	// Minimal base64 decode for test use without pulling in encoding/base64.
	return decodeBase64(b64)
}

// decodeBase64 decodes a standard base64 string.
func decodeBase64(s string) []byte {
	const dec = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	lookup := [256]byte{}
	for i := range lookup {
		lookup[i] = 0xff
	}
	for i := 0; i < len(dec); i++ {
		lookup[dec[i]] = byte(i)
	}
	// Strip padding
	stripped := s
	for len(stripped) > 0 && stripped[len(stripped)-1] == '=' {
		stripped = stripped[:len(stripped)-1]
	}
	n := len(stripped)
	out := make([]byte, 0, n*3/4+3)
	acc := 0
	bits := 0
	for i := 0; i < n; i++ {
		v := lookup[stripped[i]]
		if v == 0xff {
			continue
		}
		acc = (acc << 6) | int(v)
		bits += 6
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(acc>>bits))
			acc &= (1 << bits) - 1
		}
	}
	return out
}

// SetStepFn is a no-op for host builds.
func (c *Canvas) SetStepFn(fn func(dt float64)) {}

// TickFrame is a no-op for host builds.
func (c *Canvas) TickFrame(ts float64) {}
