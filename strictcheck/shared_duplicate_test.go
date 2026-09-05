package strictcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/transpile"
)

func TestCheckFileSharedDuplicateDiagnostic(t *testing.T) {
	for _, mode := range []string{"pure", "pure-island", "island-pure"} {
		t.Run(mode, func(t *testing.T) {
			root, page := sharedRefreshModule(t)
			a := filepath.Join(root, "app", "ui", "a.gsx")
			b := filepath.Join(root, "app", "ui", "b.gsx")
			pure := "package ui\ncomponent Badge() {\n\treturn <span>pure</span>\n}\n"
			island := "package ui\n//gosx:island\ncomponent Badge() {\n\treturn <span>island</span>\n}\n"
			first, second := pure, pure
			if mode == "pure-island" {
				second = island
			}
			if mode == "island-pure" {
				first = island
			}
			// Reverse creation order must not change sorted source reporting.
			mustWrite(t, b, second)
			mustWrite(t, a, first)
			mustWrite(t, page, "package main\nimport ui \"./ui\"\ncomponent Page() {\n\treturn <ui.Badge />\n}\n")
			want := fmt.Sprintf("duplicate shared component %q declared in %q and %q", "Badge", a, b)
			files, err := transpile.LoadPackageDir(filepath.Dir(a))
			if err != nil {
				t.Fatal(err)
			}
			forward := checkSharedComponentDuplicates(files)
			files[0], files[1] = files[1], files[0]
			backward := checkSharedComponentDuplicates(files)
			if forward == nil || backward == nil || forward.Error() != backward.Error() || !strings.Contains(forward.Error(), want) {
				t.Fatalf("order-dependent duplicate: forward=%v backward=%v", forward, backward)
			}
			for i := 0; i < 2; i++ {
				ctx, cancel := strictCheckCorpusContext(t)
				err := CheckFileWithOptions(ctx, page, Options{Env: append(os.Environ(), "GOPROXY=off", "GOSUMDB=off"), GOWORK: "off", GOFLAGS: "-mod=readonly"})
				cancel()
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("error = %v, want %s", err, want)
				}
				if strings.Contains(err.Error(), "run gosx check") {
					t.Fatalf("misleading check advice: %v", err)
				}
			}
		})
	}
}

func TestCheckFileSharedDuplicateSameFile(t *testing.T) {
	root, page := sharedRefreshModule(t)
	target := filepath.Join(root, "app", "ui", "badge.gsx")
	mustWrite(t, target, "package ui\ncomponent Badge() {\n\treturn <span>a</span>\n}\ncomponent Badge() {\n\treturn <span>b</span>\n}\n")
	mustWrite(t, page, "package main\nimport ui \"./ui\"\ncomponent Page() {\n\treturn <ui.Badge />\n}\n")
	ctx, cancel := strictCheckCorpusContext(t)
	defer cancel()
	err := CheckFileWithOptions(ctx, page, Options{Env: append(os.Environ(), "GOPROXY=off", "GOSUMDB=off"), GOWORK: "off", GOFLAGS: "-mod=readonly"})
	if err == nil || !strings.Contains(err.Error(), `duplicate component name "Badge"`) || !strings.Contains(err.Error(), target) {
		t.Fatalf("error = %v, want duplicate Badge and %s", err, target)
	}
}

func TestCheckFileSharedUniqueNeighbor(t *testing.T) {
	root, page := sharedRefreshModule(t)
	mustWrite(t, filepath.Join(root, "app", "ui", "badge.gsx"), "package ui\ncomponent Badge() {\n\treturn <span>a</span>\n}\n")
	mustWrite(t, filepath.Join(root, "app", "ui", "neighbor.gsx"), "package ui\n//gosx:island\ncomponent Neighbor() {\n\treturn <span>b</span>\n}\n")
	mustWrite(t, page, "package main\nimport ui \"./ui\"\ncomponent Page() {\n\treturn <ui.Badge />\n}\n")
	checkSharedRefresh(t, page, "unique island neighbor", "")
}
