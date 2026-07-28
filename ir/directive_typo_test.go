package ir_test

import (
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	. "m31labs.dev/gosx/ir"
)

// lowerDirective compiles a minimal component carrying the given comment and
// returns the lowering error, or nil. It does not use lowerSource from
// rawtext_test.go, because that helper fails the test on a lowering error and
// the error is exactly what these tests read.
func lowerDirective(t *testing.T, directive string) error {
	t.Helper()
	return lowerDirectiveWithRoot(t, directive, "<button>+</button>")
}

// lowerDirectiveWithRoot is lowerDirective with the component's root element
// chosen by the caller.
//
// A correctly spelled //gosx:engine surface runs lowerEngineSurface, which
// requires a <canvas> root. Lowering it over a <button> fails for that reason
// and not for the spelling, which would make the accepted-directive cases pass
// or fail for the wrong reason.
func lowerDirectiveWithRoot(t *testing.T, directive, root string) error {
	t.Helper()
	src := "package app\n\n" + directive + "\nfunc Counter(props any) Node {\n\treturn " + root + "\n}\n"
	tree, lang, err := gosx.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", directive, err)
	}
	_, lowerErr := Lower(tree.RootNode(), []byte(src), lang)
	return lowerErr
}

// TestNearMissDirectivesAreReported is the reason this check exists.
//
// Each spelling below silently produced a static component whose handlers
// never run, and `gosx check` reported "ok" for every one of them.
func TestNearMissDirectivesAreReported(t *testing.T) {
	cases := []struct {
		name      string
		directive string
	}{
		{"space after slashes", "// gosx:island"},
		{"space before colon", "//gosx :island"},
		{"space after colon", "//gosx: island"},
		{"spaces everywhere", "//  gosx : island"},
		{"capital directive", "//gosx:Island"},
		{"capital prefix", "//GoSX:island"},
		{"tab after slashes", "//\tgosx:island"},
		{"engine with a space", "// gosx:engine worker"},
		{"engine capitalized", "//gosx:Engine surface"},
		{"engine video", "// gosx:engine video"},
		{"capabilities with a space", "// gosx:capabilities webgpu"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := lowerDirective(t, tc.directive)
			if err == nil {
				t.Fatalf("lowering %q reported no error; the directive is silently ignored", tc.directive)
			}
			// The diagnostic prints the line with %q, so compare against the
			// quoted form. A raw comparison passes for every case except one
			// containing a tab, which %q escapes.
			message := err.Error()
			quoted := strconv.Quote(strings.TrimSpace(tc.directive))
			if !strings.Contains(message, quoted) {
				t.Errorf("error does not quote the offending line %s.\ngot: %s", quoted, message)
			}
			if !strings.Contains(message, "exactly as") {
				t.Errorf("error gives no corrected spelling.\ngot: %s", message)
			}
		})
	}
}

// TestCorrectDirectivesAreAccepted is the other half. A check that rejected
// every comment would satisfy the test above and break every real component.
func TestCorrectDirectivesAreAccepted(t *testing.T) {
	cases := []struct {
		name      string
		directive string
		root      string
	}{
		{"island", "//gosx:island", "<button>+</button>"},
		{"engine worker", "//gosx:engine worker", "<button>+</button>"},
		{"engine video", "//gosx:engine video", "<button>+</button>"},
		{"engine surface", "//gosx:engine surface", "<canvas></canvas>"},
		{"engine with capabilities", "//gosx:engine surface\n//gosx:capabilities webgpu", "<canvas></canvas>"},
		{"no directive at all", "// An ordinary component.", "<button>+</button>"},
		{"no comment at all", "", "<button>+</button>"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := lowerDirectiveWithRoot(t, tc.directive, tc.root); err != nil {
				t.Errorf("lowering %q failed: %v", tc.directive, err)
			}
		})
	}
}

// TestProseAboutDirectivesIsNotReported guards the false-positive direction.
//
// grammar.go already carries "// gosx:island directive on component". Prose
// that names a directive must stay legal, or this check would reject the code
// that documents it. Every case here is a near miss by prefix and a miss by
// shape, which is the distinction the check is built on.
func TestProseAboutDirectivesIsNotReported(t *testing.T) {
	cases := []struct {
		name      string
		directive string
	}{
		{"island named in prose", "// gosx:island directive on component"},
		{"island described", "// The gosx:island marker enables hydration."},
		{"engine with an unknown kind", "// gosx:engine explains the surface lowering"},
		{"capabilities with no list", "// gosx:capabilities"},
		{"a different namespace", "// go:generate stringer -type=Kind"},
		{"a build tag", "//go:build !tinygo"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := lowerDirective(t, tc.directive); err != nil {
				t.Errorf("prose %q was reported as a typo: %v", tc.directive, err)
			}
		})
	}
}
