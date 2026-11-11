package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestDocsFolderHoldsOnlyIndexedUserGuides keeps specs, plans, roadmaps and
// reports out of docs/. docs/ holds user guides and references only, and
// docs/README.md is the list of them: every Markdown file in docs/ must be
// linked from that index, and nothing else may live there.
func TestDocsFolderHoldsOnlyIndexedUserGuides(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	docs := filepath.Join(filepath.Dir(thisFile), "..", "..", "docs")

	index, err := os.ReadFile(filepath.Join(docs, "README.md"))
	if err != nil {
		t.Fatalf("docs/README.md is the index of user guides: %v", err)
	}
	linked := map[string]bool{}
	for _, m := range regexp.MustCompile(`\]\(([\w.-]+\.md)[)#]`).FindAllStringSubmatch(string(index), -1) {
		linked[m[1]] = true
	}

	banned := regexp.MustCompile(`(?i)(^|[-_])(spec|specs|plan|plans|roadmap|design|research|status|qa|report|handoff|adr)([-_.]|$)`)
	entries, err := os.ReadDir(docs)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir():
			t.Errorf("docs/%s/: docs/ holds flat user guides only; specs and plans live in Hyphae", name)
		case !strings.HasSuffix(name, ".md"):
			t.Errorf("docs/%s: only Markdown user guides belong in docs/", name)
		case name == "README.md":
		case banned.MatchString(name):
			t.Errorf("docs/%s looks like a spec, plan or report; those live in Hyphae, not in the repo", name)
		case !linked[name]:
			t.Errorf("docs/%s is not linked from docs/README.md; add it to the index if it is a user guide", name)
		}
	}
	for name := range linked {
		if _, err := os.Stat(filepath.Join(docs, name)); err != nil {
			t.Errorf("docs/README.md links docs/%s, which does not exist", name)
		}
	}
}
