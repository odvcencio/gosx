package scene

// Defer mounts a Scene3D after first paint and idle. The page paints the
// mount's poster or fallback children first. The runtime downloads no scene
// chunk, props or model until the moment Until names has passed and every
// RequiredCapabilities gate holds. See docs/scene3d-performance "Deferred scenes".
type Defer struct {
	// Until is "idle" (the default: two animation frames after the engines
	// chunk ran, then requestIdleCallback) or "load" (the window load event,
	// then the same idle wait).
	Until string `json:"until,omitempty"`
	// TimeoutMS caps the idle wait. Zero means 1800.
	TimeoutMS int `json:"timeoutMs,omitempty"`
	// DelayMS waits this long after Until before the idle wait. Zero means none.
	DelayMS int `json:"delayMs,omitempty"`
	// Inline keeps the props in the page manifest instead of a
	// content-addressed URL. A static export needs it; a server does not.
	Inline bool `json:"inline,omitempty"`
	// Variants pick a different props snapshot by media query; the first
	// match wins. The server stores each variant content-addressed.
	Variants []DeferVariant `json:"-"`
}

// DeferVariant is one media-query alternative of a deferred scene.
type DeferVariant struct {
	Media string
	Props *Props
}
