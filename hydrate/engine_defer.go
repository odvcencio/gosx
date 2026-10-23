package hydrate

import "m31labs.dev/gosx/internal/urlpath"

// EngineDefer tells the browser to mount a Scene3D after first paint and
// idle instead of at bootstrap. The server fills Preloads from the scene the
// same way it fills the head preloads of an eager scene.
type EngineDefer struct {
	Until     string `json:"until,omitempty"`
	TimeoutMS int    `json:"timeoutMs,omitempty"`
	DelayMS   int    `json:"delayMs,omitempty"`
	// Preloads are the chunk and asset hints the runtime emits when the
	// deferred load starts. The scene chunk and the renderer chunk are not
	// listed; the runtime adds them from the bootstrap tag.
	Preloads []EnginePreload `json:"preloads,omitempty"`
	// Variants select another props snapshot by media query, first match
	// wins; otherwise the entry's PropsRef is used.
	Variants []EngineDeferVariant `json:"variants,omitempty"`
}

// EnginePreload is one <link rel="preload"> the runtime emits.
type EnginePreload struct {
	Href string `json:"href"`
	As   string `json:"as"`
}

// EngineDeferVariant pairs a media query with a props URL.
type EngineDeferVariant struct {
	Media    string `json:"media"`
	PropsRef string `json:"propsRef"`
}

func engineDeferWithBasePath(prefix string, d *EngineDefer) *EngineDefer {
	if d == nil {
		return nil
	}
	out := *d
	out.Preloads = append([]EnginePreload(nil), d.Preloads...)
	for i := range out.Preloads {
		out.Preloads[i].Href = urlpath.URL(prefix, out.Preloads[i].Href)
	}
	out.Variants = append([]EngineDeferVariant(nil), d.Variants...)
	for i := range out.Variants {
		out.Variants[i].PropsRef = urlpath.URL(prefix, out.Variants[i].PropsRef)
	}
	return &out
}
