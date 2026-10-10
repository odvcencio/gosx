package wire

import (
	"html"
	"testing"
)

func TestReferencesDocumentTreeEmbeddings(t *testing.T) {
	for _, tc := range []struct {
		name, body     string
		child, runtime bool
	}{
		{"srcdoc-fetched-child", `<iframe srcdoc="` + html.EscapeString(`<iframe src="/child/"></iframe>`) + `"></iframe>`, true, false},
		{"srcdoc-overrides-src", `<iframe src="/child/" srcdoc="Static"></iframe>`, false, false},
		{"sandbox-fetched-document", `<iframe sandbox src="/child/"></iframe>`, true, false},
		{"active-srcdoc-script", `<iframe srcdoc="` + html.EscapeString(`<script src="/runtime.js"></script>`) + `"></iframe>`, false, true},
		{"sandbox-srcdoc-script", `<iframe sandbox srcdoc="` + html.EscapeString(`<script src="/runtime.js"></script>`) + `"></iframe>`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ScanReferences([]byte(tc.body), KindDocument)
			if err != nil || !got.Complete {
				t.Fatal("document tree unresolved", got, err)
			}
			child, runtime := false, false
			for _, r := range got.Resources {
				child = child || r.URL == "/child/"
				runtime = runtime || r.URL == "/runtime.js"
			}
			if child != tc.child || runtime != tc.runtime {
				t.Fatalf("child/runtime=%v/%v want=%v/%v", child, runtime, tc.child, tc.runtime)
			}
		})
	}
}
