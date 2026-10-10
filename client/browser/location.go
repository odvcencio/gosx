package browser

// Location is a snapshot of the current browser URL. Read it when navigating
// or opening a connection, rather than repeatedly during a render frame.
type Location struct {
	Href, Origin, Protocol, Host, Path, Search, Hash string
}

// Size describes a CSS viewport in pixels.
type Size struct{ Width, Height float64 }

// JSONCapture describes an externally installed, opt-in diagnostic observer.
// It never installs the observer or enables capture. Enabled should be checked
// before encoding a potentially large payload. Publication updates the payload,
// label and monotonically incremented capture sequence together.
type JSONCapture struct {
	GlobalName, EnabledFlag, PayloadField, LabelField, SequenceField string
}
