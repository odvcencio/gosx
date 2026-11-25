package mediatype

import "testing"

func TestForServingAddsCharsetToText(t *testing.T) {
	cases := map[string]string{
		".js":          "text/javascript; charset=utf-8",
		".css":         "text/css; charset=utf-8",
		".wgsl":        "text/plain; charset=utf-8",
		".webmanifest": "application/manifest+json; charset=utf-8",
		".glb":         "model/gltf-binary",
		".ktx2":        "image/ktx2",
		".json":        "application/json",
	}
	for ext, want := range cases {
		if got := ForServing(ext); got != want {
			t.Errorf("ForServing(%q) = %q, want %q", ext, got, want)
		}
	}
}

func TestByExtensionIsCaseBlind(t *testing.T) {
	for _, ext := range []string{".GLB", ".Glb", ".glb"} {
		if got := ByExtension(ext); got != "model/gltf-binary" {
			t.Errorf("ByExtension(%q) = %q", ext, got)
		}
	}
	if got := ByExtension(".nosuchthing"); got != "" {
		t.Errorf("unknown extension gave %q", got)
	}
}

// The 29 pairs the single-file exporter listed at 0834774d65, written out so
// the test holds after the exporter's private copy is gone.
func TestTableCoversTheExporterTypes(t *testing.T) {
	pairs := [][2]string{
		{".wasm", "application/wasm"}, {".js", "text/javascript"}, {".mjs", "text/javascript"},
		{".css", "text/css"}, {".json", "application/json"}, {".webmanifest", "application/manifest+json"},
		{".txt", "text/plain"}, {".svg", "image/svg+xml"}, {".png", "image/png"}, {".jpg", "image/jpeg"},
		{".jpeg", "image/jpeg"}, {".gif", "image/gif"}, {".webp", "image/webp"}, {".avif", "image/avif"},
		{".ico", "image/x-icon"}, {".glb", "model/gltf-binary"}, {".gltf", "model/gltf+json"},
		{".bin", "application/octet-stream"}, {".woff", "font/woff"}, {".woff2", "font/woff2"},
		{".ttf", "font/ttf"}, {".otf", "font/otf"}, {".mp3", "audio/mpeg"}, {".ogg", "audio/ogg"},
		{".wav", "audio/wav"}, {".mp4", "video/mp4"}, {".webm", "video/webm"}, {".ktx2", "image/ktx2"},
		{".wgsl", "text/plain"},
	}
	if len(pairs) != 29 {
		t.Fatalf("%d pairs, want 29", len(pairs))
	}
	for _, p := range pairs {
		if got := ByExtension(p[0]); got != p[1] {
			t.Errorf("ByExtension(%q) = %q, want %q", p[0], got, p[1])
		}
	}
}
