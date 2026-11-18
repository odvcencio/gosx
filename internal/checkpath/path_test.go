package checkpath

import (
	"path/filepath"
	"testing"

	"golang.org/x/mod/module"
)

func TestSanitize(t *testing.T) {
	seen := map[string]string{}
	for _, path := range []string{"app/[code]", "app/[...slug]", "app/[[optional]]", "app/(marketing)", "app/p5b636f64655d", "app/_code", "app/__slug", "app/a/b", "app/a-b"} {
		got := filepath.ToSlash(Sanitize(filepath.FromSlash(path)))
		if err := module.CheckImportPath("example.com/" + got); err != nil {
			t.Fatalf("Sanitize(%q) = %q: %v", path, got, err)
		}
		if previous, ok := seen[got]; ok {
			t.Fatalf("Sanitize(%q) and Sanitize(%q) collide: %q", previous, path, got)
		}
		seen[got] = path
	}
}
