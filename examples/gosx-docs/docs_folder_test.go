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
// linked from that index. Only an images/ folder may sit beside the guides.
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

	// Names that mark an intent or history document. "design", "status",
	// "plan" and "report" are allowed: they name real features and guides.
	banned := regexp.MustCompile(`(?i)^\d{4}-\d{2}-\d{2}-|(^|[-_])(spec|specs|roadmap|research|handoff|adr|proposal|rfc|phase|notes)([-_.]|$)`)
	statusBanner := regexp.MustCompile(`(?im)^>?\s*\**(status|phase)\**\s*:\s*[^=\s]`)
	taskList := regexp.MustCompile(`(?m)^\s*- \[ \] `)
	entries, err := os.ReadDir(docs)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir() && name == "images":
		case e.IsDir():
			t.Errorf("docs/%s/: docs/ holds flat user guides only; specs and plans live in Hyphae", name)
		case name == "embed.go":
			// embeds the guides for the docs site; see docs/embed.go
		case !strings.HasSuffix(name, ".md"):
			t.Errorf("docs/%s: only Markdown user guides belong in docs/", name)
		case name == "README.md":
		case banned.MatchString(name):
			t.Errorf("docs/%s looks like a spec, plan or history document; those live in Hyphae, not in the repo", name)
		case !linked[name]:
			t.Errorf("docs/%s is not linked from docs/README.md; add it to the index if it is a user guide", name)
		default:
			body, err := os.ReadFile(filepath.Join(docs, name))
			if err != nil {
				t.Fatal(err)
			}
			if statusBanner.Match(body) {
				t.Errorf("docs/%s has a Status: banner; guides describe what works now, not project status", name)
			}
			if taskList.Match(body) {
				t.Errorf("docs/%s has an open task list (- [ ]); task lists are plans and live in Hyphae", name)
			}
		}
	}
	for name := range linked {
		if _, err := os.Stat(filepath.Join(docs, name)); err != nil {
			t.Errorf("docs/README.md links docs/%s, which does not exist", name)
		}
	}
}

// TestCIGatesDocNamesEveryHostedMakeTarget keeps docs/ci-gates.md in step with
// the workflow: each `run: make <target>` in ci.yml must appear in the gate
// table.
func TestCIGatesDocNamesEveryHostedMakeTarget(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Skipf("workflow not available: %v", err)
	}
	doc, err := os.ReadFile(filepath.Join(root, "docs", "ci-gates.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range regexp.MustCompile(`run: make ([\w-]+)`).FindAllStringSubmatch(string(workflow), -1) {
		if !strings.Contains(string(doc), "`"+m[1]) {
			t.Errorf("ci.yml runs `make %s`, which docs/ci-gates.md does not mention", m[1])
		}
	}
}
