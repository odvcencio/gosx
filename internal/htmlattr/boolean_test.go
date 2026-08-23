package htmlattr

import "testing"

func TestIsBooleanUsesHTMLNamesCaseInsensitively(t *testing.T) {
	for _, name := range []string{"hidden", "readOnly", "allowFullScreen", " SELECTED "} {
		if !IsBoolean(name) {
			t.Errorf("IsBoolean(%q) = false", name)
		}
	}
	for _, name := range []string{"aria-pressed", "spellcheck", "contenteditable", "download"} {
		if IsBoolean(name) {
			t.Errorf("IsBoolean(%q) = true", name)
		}
	}
}
