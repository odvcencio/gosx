package pagecaps

import (
	"html"
	"reflect"
	"testing"

	"m31labs.dev/gosx/internal/pagecaps/embeddingtest"
)

func TestDocumentTreeEmbeddingOracleCoversWalker(t *testing.T) {
	covered := map[string]bool{}
	for _, element := range embeddingtest.Elements {
		if covered[element.Name] || element.Spec == "" {
			t.Fatal("embedding oracle needs unique names and HTML spec citations")
		}
		covered[element.Name] = true
	}
	if !reflect.DeepEqual(documentEmbeddingElements, covered) {
		t.Fatalf("walker embeddings %v need a matching spec-backed capability table: %v", documentEmbeddingElements, covered)
	}
}

func TestDocumentTreeEmbeddingPermissions(t *testing.T) {
	const child = `<script type="module">run()</script>`
	for _, element := range embeddingtest.Elements {
		for _, mode := range []string{"src", "srcdoc", "empty-srcdoc"} {
			for _, sandbox := range []string{"", "sandbox", `sandbox="allow-scripts"`} {
				for _, parentBlocked := range []bool{false, true} {
					name := element.Name + "/" + mode + "/" + sandbox
					if parentBlocked {
						name += "/restricted-parent"
					}
					t.Run(name, func(t *testing.T) {
						attrs := sandbox + ` src="/child/"`
						if mode == "srcdoc" {
							attrs += ` srcdoc="` + html.EscapeString(child) + `"`
						} else if mode == "empty-srcdoc" {
							attrs += ` srcdoc=""`
						}
						inner := element.Document(element.Markup(attrs))
						parent := ""
						if parentBlocked {
							parent = "sandbox"
						}
						body := embeddingtest.Lookup("iframe").Markup(parent + ` srcdoc="` + html.EscapeString(inner) + `"`)
						loads := 0
						tree, err := ParseDocumentTree([]byte(body), "/root/", func(base, reference string) ([]byte, string, bool, error) {
							if reference != "/child/" {
								t.Fatalf("unexpected embedding source %s", reference)
							}
							loads++
							return []byte(child), "/child/", true, nil
						})
						if err != nil || !tree.Complete {
							t.Fatal("embedding tree incomplete", err)
						}
						inline := mode != "src" && element.Srcdoc
						wantLoads := 0
						if !inline && element.Src {
							wantLoads = 1
						}
						active := !parentBlocked && (!element.Sandbox || sandbox != "sandbox") &&
							(inline && mode == "srcdoc" || !inline && element.Src)
						sources := 0
						_, err = InspectDocumentTree(tree, func(_ *Document, source ExecutableSource) { sources++ })
						wantSources := 0
						if active {
							wantSources = 1
						}
						if err != nil || sources != wantSources || loads != wantLoads {
							t.Fatalf("sources/loads=%d/%d want=%d/%d err=%v (%s)", sources, loads, wantSources, wantLoads, err, element.Spec)
						}
					})
				}
			}
		}
	}
}
