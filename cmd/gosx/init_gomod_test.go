package main

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// TestScaffoldGoDirectiveAdmitsGoSX pins the generated go.mod to the floor GoSX
// itself declares.
//
// A scaffolded project requires m31labs.dev/gosx, so it inherits that floor
// whatever its own go directive says. When the scaffold declares less, the
// second command a new user runs fails:
//
//	$ gosx init my-app && cd my-app && go run .
//	go: module .../m31labs.dev/gosx@v0.36.0 requires go >= 1.26 (running go 1.25.1)
//
// That is exactly what happened: go.mod moved to 1.26 and this template kept a
// hardcoded 1.25.1.
func TestScaffoldGoDirectiveAdmitsGoSX(t *testing.T) {
	template := goModTemplate("example.com/scaffolded")

	directive := regexp.MustCompile(`(?m)^go (\d+\.\d+(?:\.\d+)?)$`)
	match := directive.FindStringSubmatch(template)
	if match == nil {
		t.Fatalf("generated go.mod declares no go directive:\n%s", template)
	}

	scaffold, floor := match[1], gosx.MinGoVersion
	if compareGoVersions(t, scaffold, floor) < 0 {
		t.Errorf("scaffold declares go %s, GoSX requires go %s\n"+
			"`go run .` fails in a fresh project.", scaffold, floor)
	}
}

// TestScaffoldRequiresTheCurrentRelease keeps the other half of the template
// honest: the generated require must name the version this CLI ships.
func TestScaffoldRequiresTheCurrentRelease(t *testing.T) {
	template := goModTemplate("example.com/scaffolded")
	want := "require m31labs.dev/gosx v" + gosx.Version
	if !strings.Contains(template, want) {
		t.Errorf("generated go.mod lacks %q:\n%s", want, template)
	}
}

// compareGoVersions orders two go directives numerically, so 1.9 sorts below
// 1.26 where a string comparison would not.
func compareGoVersions(t *testing.T, a, b string) int {
	t.Helper()
	fields := func(v string) []int {
		parts := strings.Split(v, ".")
		out := make([]int, 3)
		for i := 0; i < len(parts) && i < 3; i++ {
			n, err := strconv.Atoi(parts[i])
			if err != nil {
				t.Fatalf("go version %q has a non-numeric component %q", v, parts[i])
			}
			out[i] = n
		}
		return out
	}
	left, right := fields(a), fields(b)
	for i := range left {
		switch {
		case left[i] < right[i]:
			return -1
		case left[i] > right[i]:
			return 1
		}
	}
	return 0
}
