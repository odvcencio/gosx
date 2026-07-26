package field

// Scratch is reusable working memory for the operators that need a temporary
// buffer. Blur is the only operator that needs one today.
//
// Allocate one Scratch and pass it to every call in a simulation loop. The
// buffer grows to the largest shape it has seen and is never freed until the
// Scratch itself goes out of scope. A loop that keeps its shape therefore
// allocates nothing after the first step.
//
// A Scratch is not safe for concurrent use. Give each goroutine its own.
type Scratch struct {
	buf  []float32
	view Field

	// kernel caches the last Gaussian kernel so a loop with a fixed radius
	// builds it once.
	kernel       []float32
	kernelRadius float32
	kernelSet    bool
}

// NewScratch returns a Scratch pre-sized for one field of the given shape.
// Passing a zero resolution is legal; the buffer then grows on first use.
func NewScratch(resolution [3]int, components int) *Scratch {
	s := &Scratch{}
	n := resolution[0] * resolution[1] * resolution[2] * components
	if n > 0 {
		s.buf = make([]float32, n)
	}
	return s
}

// fieldLike returns a Field that shares the scratch buffer and copies the shape
// of like. The returned pointer stays valid until the next fieldLike call on the
// same Scratch. Callers must not keep it.
func (s *Scratch) fieldLike(like *Field) *Field {
	n := like.Resolution[0] * like.Resolution[1] * like.Resolution[2] * like.Components
	if cap(s.buf) < n {
		s.buf = make([]float32, n)
	}
	s.buf = s.buf[:n]
	s.view.Resolution = like.Resolution
	s.view.Components = like.Components
	s.view.Bounds = like.Bounds
	s.view.Data = s.buf
	return &s.view
}

// gaussian returns the cached Gaussian kernel for the radius, and builds it
// when the radius changed since the last call.
func (s *Scratch) gaussian(radius float32) []float32 {
	if s.kernelSet && s.kernelRadius == radius {
		return s.kernel
	}
	s.kernel = gaussianKernel(radius)
	s.kernelRadius = radius
	s.kernelSet = true
	return s.kernel
}

// Cap returns the number of float32 values the scratch buffer holds. Use it in
// tests and diagnostics to confirm that a loop reached a steady state.
func (s *Scratch) Cap() int {
	if s == nil {
		return 0
	}
	return cap(s.buf)
}

// sameBuffer reports whether two fields share the first element of their data.
// Operators use it to pick a pass order that does not read a buffer after it has
// been overwritten.
func sameBuffer(a, b *Field) bool {
	if a == b {
		return true
	}
	if len(a.Data) == 0 || len(b.Data) == 0 {
		return false
	}
	return &a.Data[0] == &b.Data[0]
}

// validateOutput checks that dst can hold a result of the given shape.
func validateOutput(op string, dst *Field, resolution [3]int, components int) error {
	if dst == nil {
		return fieldError(op, "output field is nil")
	}
	if err := validateField(op, dst); err != nil {
		return err
	}
	if dst.Resolution != resolution || dst.Components != components {
		return fieldError(op, "output shape mismatch: %v/%d != %v/%d",
			dst.Resolution, dst.Components, resolution, components)
	}
	return nil
}
