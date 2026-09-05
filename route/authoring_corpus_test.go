package route

import (
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/gsxcorpus"
)

func TestAuthoringCorpusRouteRenderMatrix(t *testing.T) {
	cases, err := gsxcorpus.Load()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, tc := range cases {
		if tc.Route == gsxcorpus.Skip {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			root := t.TempDir()
			if err := tc.WriteTo(root); err != nil {
				t.Fatal(err)
			}
			node, err := DefaultFileRenderer(nil, FilePage{FilePath: tc.MainPath(root), Pattern: "/"})
			assertCorpusExpectation(t, "route", tc.Route, tc.ErrorContains("route"), err)
			if err != nil {
				return
			}
			html := gosx.RenderHTML(node)
			if html != tc.RouteHTML {
				t.Fatalf("route html = %q, want %q", html, tc.RouteHTML)
			}
			nodeAgain, err := DefaultFileRenderer(nil, FilePage{FilePath: tc.MainPath(root), Pattern: "/"})
			if err != nil {
				t.Fatalf("route second render: %v", err)
			}
			if htmlAgain := gosx.RenderHTML(nodeAgain); htmlAgain != html {
				t.Fatalf("route output is not deterministic\nfirst:  %q\nsecond: %q", html, htmlAgain)
			}
			if strings.Contains(html, `slot=`) {
				t.Fatalf("slot routing marker leaked into route HTML: %s", html)
			}
			if !filepath.IsAbs(tc.MainPath(root)) {
				t.Fatalf("fixture path is not absolute: %s", tc.MainPath(root))
			}
			checked++
		})
	}
	if checked == 0 {
		t.Fatal("no route corpus cases ran")
	}
}

func assertCorpusExpectation(t *testing.T, target string, want gsxcorpus.Expectation, contains string, err error) {
	t.Helper()
	switch want {
	case gsxcorpus.Pass:
		if err != nil {
			t.Fatalf("%s error = %v, want pass", target, err)
		}
	case gsxcorpus.Reject:
		if err == nil {
			t.Fatalf("%s succeeded, want reject", target)
		}
		if contains != "" && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(contains)) {
			t.Fatalf("%s error = %v, want substring %q", target, err, contains)
		}
	case gsxcorpus.Skip:
	default:
		t.Fatalf("unknown expectation %q for %s", want, target)
	}
}
