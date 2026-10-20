package wire

import (
	"html"
	"testing"

	"m31labs.dev/gosx/internal/pagecaps/embeddingtest"
)

func TestReferencesDocumentTreeEmbeddings(t *testing.T) {
	for _, element := range embeddingtest.Elements {
		wrap := func(attrs string) string { return element.Document(element.Markup(attrs)) }
		for _, tc := range []struct {
			name, body     string
			child, runtime bool
		}{
			{"srcdoc-fetched-child", embeddingtest.Lookup("iframe").Markup(`srcdoc="` + html.EscapeString(wrap(`src="/child/"`)) + `"`), element.Src, false},
			{"srcdoc-overrides-src", wrap(`src="/child/" srcdoc="Static"`), element.Src && !element.Srcdoc, false},
			{"empty-srcdoc-overrides-src", wrap(`src="/child/" srcdoc=""`), element.Src && !element.Srcdoc, false},
			{"sandbox-fetched-document", wrap(`sandbox src="/child/"`), element.Src, false},
			{"active-srcdoc-script", wrap(`src="/child/" srcdoc="` + html.EscapeString(`<script src="/runtime.js"></script>`) + `"`), element.Src && !element.Srcdoc, element.Srcdoc},
			{"sandbox-srcdoc-script", wrap(`sandbox src="/child/" srcdoc="` + html.EscapeString(`<script src="/runtime.js"></script>`) + `"`), element.Src && !element.Srcdoc, element.Srcdoc && !element.Sandbox},
		} {
			t.Run(element.Name+"/"+tc.name, func(t *testing.T) {
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
}
