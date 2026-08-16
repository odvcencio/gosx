package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionSkewError(t *testing.T) {
	cases := []struct {
		name            string
		cliVersion      string
		projectVersion  string
		hasLocalReplace bool
		wantErr         string
	}{
		{
			name:           "matching versions pass",
			cliVersion:     "v0.41.0",
			projectVersion: "v0.41.0",
		},
		{
			name:           "no gosx dependency skips",
			cliVersion:     "v0.41.0",
			projectVersion: "",
		},
		{
			name:            "local replace skips even on skew",
			cliVersion:      "v0.41.0",
			projectVersion:  "v0.31.4",
			hasLocalReplace: true,
		},
		{
			name:           "older project fails with the install hint",
			cliVersion:     "v0.38.1",
			projectVersion: "v0.31.4",
			wantErr:        "gosx v0.38.1 cannot operate on a project pinned to m31labs.dev/gosx v0.31.4. Run: go install m31labs.dev/gosx/cmd/gosx@v0.31.4, or set GOSX_SKIP_VERSION_CHECK=1 to override",
		},
		{
			name:           "newer project also fails",
			cliVersion:     "v0.31.4",
			projectVersion: "v0.38.1",
			wantErr:        "gosx v0.31.4 cannot operate on a project pinned to m31labs.dev/gosx v0.38.1. Run: go install m31labs.dev/gosx/cmd/gosx@v0.38.1, or set GOSX_SKIP_VERSION_CHECK=1 to override",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := versionSkewError(tc.cliVersion, tc.projectVersion, tc.hasLocalReplace)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("versionSkewError(%q, %q, %v) = %v, want nil", tc.cliVersion, tc.projectVersion, tc.hasLocalReplace, err)
				}
				return
			}
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("versionSkewError(%q, %q, %v) = %v, want %q", tc.cliVersion, tc.projectVersion, tc.hasLocalReplace, err, tc.wantErr)
			}
		})
	}
}

// TestVersionSkewInterpretGoListModule exercises the pure decision behind
// resolveProjectGoSXVersion directly against synthetic `go list -m -json`
// records, without shelling out to the toolchain.
func TestVersionSkewInterpretGoListModule(t *testing.T) {
	cases := []struct {
		name        string
		mod         goListModule
		wantVersion string
		wantLocal   bool
		wantOK      bool
	}{
		{
			name:        "plain required version",
			mod:         goListModule{Version: "v0.31.4"},
			wantVersion: "v0.31.4",
			wantOK:      true,
		},
		{
			name:      "path replace has no replacement version",
			mod:       goListModule{Version: "v0.31.4", Replace: &goListModule{Dir: "/home/dev/gosx"}},
			wantLocal: true,
			wantOK:    true,
		},
		{
			name:        "module-version replace compares the replaced version",
			mod:         goListModule{Version: "v0.31.4", Replace: &goListModule{Version: "v0.30.7"}},
			wantVersion: "v0.30.7",
			wantOK:      true,
		},
		{
			name:      "workspace-local module has a dir but no version",
			mod:       goListModule{Dir: "/home/dev/gosx"},
			wantLocal: true,
			wantOK:    true,
		},
		{
			name: "neither version nor dir is unknown",
			mod:  goListModule{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			version, localReplace, ok := interpretGoListModule(tc.mod)
			if version != tc.wantVersion || localReplace != tc.wantLocal || ok != tc.wantOK {
				t.Fatalf("interpretGoListModule(%+v) = (%q, %v, %v), want (%q, %v, %v)",
					tc.mod, version, localReplace, ok, tc.wantVersion, tc.wantLocal, tc.wantOK)
			}
		})
	}
}

// newGoModProject writes a standalone go.mod (no go.sum, no source files —
// `go list -m` needs neither) and returns its directory.
func newGoModProject(t *testing.T, goMod string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return dir
}

// TestVersionSkewResolveProjectGoModVariants runs the real go toolchain
// against go.mod shapes the old hand-rolled scanner mishandled: block-form
// replace (both a local directory and a module-version target) and a
// require( block with no space before the paren and a trailing comment on
// the open line.
func TestVersionSkewResolveProjectGoModVariants(t *testing.T) {
	localTarget := newGoModProject(t, "module m31labs.dev/gosx\n\ngo 1.26\n")

	cases := []struct {
		name        string
		goMod       string
		wantVersion string
		wantLocal   bool
		wantOK      bool
	}{
		{
			name: "block-form replace to a local directory",
			goMod: `module example.com/app

go 1.26

require (
	m31labs.dev/gosx v0.31.4
)

replace (
	m31labs.dev/gosx => ` + localTarget + `
)
`,
			wantLocal: true,
			wantOK:    true,
		},
		{
			name: "block-form replace to another module version",
			goMod: `module example.com/app

go 1.26

require (
	m31labs.dev/gosx v0.31.4
)

replace (
	m31labs.dev/gosx v0.31.4 => m31labs.dev/gosx v0.30.7
)
`,
			wantVersion: "v0.30.7",
			wantOK:      true,
		},
		{
			name: "require block with no space and a trailing comment",
			goMod: `module example.com/app

go 1.26

require( // pinned deps
	m31labs.dev/gosx v0.31.4
)
`,
			wantVersion: "v0.31.4",
			wantOK:      true,
		},
		{
			name: "gosx not required at all",
			goMod: `module example.com/app

go 1.26

require github.com/other/thing v1.0.0
`,
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newGoModProject(t, tc.goMod)
			version, localReplace, ok := resolveProjectGoSXVersion(dir)
			if version != tc.wantVersion || localReplace != tc.wantLocal || ok != tc.wantOK {
				t.Fatalf("resolveProjectGoSXVersion(%q) = (%q, %v, %v), want (%q, %v, %v)",
					dir, version, localReplace, ok, tc.wantVersion, tc.wantLocal, tc.wantOK)
			}
		})
	}
}

// TestVersionSkewResolveProjectWorkspace builds a real go.work workspace with
// two member modules — an app module that requires a tagged m31labs.dev/gosx
// release, and a second member whose own module path is m31labs.dev/gosx —
// and confirms resolution follows the workspace override rather than the
// require line. The old scanner read GOMOD (which still names the app
// module's go.mod under a workspace) and reported the stale tagged version.
func TestVersionSkewResolveProjectWorkspace(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	gosxDir := filepath.Join(root, "gosx")
	for _, dir := range []string{appDir, gosxDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	mustWriteFile(t, filepath.Join(appDir, "go.mod"), `module example.com/app

go 1.26

require m31labs.dev/gosx v0.31.4
`)
	mustWriteFile(t, filepath.Join(gosxDir, "go.mod"), "module m31labs.dev/gosx\n\ngo 1.26\n")
	mustWriteFile(t, filepath.Join(root, "go.work"), `go 1.26

use ./app
use ./gosx
`)

	version, localReplace, ok := resolveProjectGoSXVersion(appDir)
	if !ok || !localReplace || version != "" {
		t.Fatalf("resolveProjectGoSXVersion(%q) = (%q, %v, %v), want (\"\", true, true) for a workspace-local module",
			appDir, version, localReplace, ok)
	}

	if err := checkVersionSkew(appDir); err != nil {
		t.Fatalf("checkVersionSkew(%q) = %v, want nil under a workspace override", appDir, err)
	}
}

func TestCheckVersionSkewSkipsOutsideAModule(t *testing.T) {
	dir := t.TempDir()
	if err := checkVersionSkew(dir); err != nil {
		t.Fatalf("checkVersionSkew(%q) = %v, want nil outside any module", dir, err)
	}
}

func TestCheckVersionSkewMatchesCLI(t *testing.T) {
	dir := newInvalidStrictStarter(t, "version-skew-match")
	if err := checkVersionSkew(dir); err != nil {
		t.Fatalf("checkVersionSkew(%q) = %v, want nil for a project on this CLI's own version with a local replace", dir, err)
	}
}

func TestCheckVersionSkewErrorMentionsInstallCommand(t *testing.T) {
	err := versionSkewError("v0.38.1", "v0.31.4", false)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "go install m31labs.dev/gosx/cmd/gosx@v0.31.4") {
		t.Fatalf("error %q does not name the install command", err.Error())
	}
}

func TestCheckVersionSkewErrorMentionsSkipEnvVar(t *testing.T) {
	err := versionSkewError("v0.38.1", "v0.31.4", false)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "GOSX_SKIP_VERSION_CHECK=1 to override") {
		t.Fatalf("error %q does not mention the escape hatch", err.Error())
	}
}

// TestCheckVersionSkewSkipEnvVar confirms GOSX_SKIP_VERSION_CHECK bypasses
// the check entirely, even for a project pinned to a version this CLI does
// not share and with no replace directive to excuse it.
func TestCheckVersionSkewSkipEnvVar(t *testing.T) {
	dir := newGoModProject(t, `module example.com/app

go 1.26

require m31labs.dev/gosx v0.30.7
`)

	if err := checkVersionSkew(dir); err == nil {
		t.Fatal("expected checkVersionSkew to fail on a genuine version pin before the escape hatch is set")
	}

	t.Setenv("GOSX_SKIP_VERSION_CHECK", "1")
	if err := checkVersionSkew(dir); err != nil {
		t.Fatalf("checkVersionSkew(%q) = %v, want nil with GOSX_SKIP_VERSION_CHECK set", dir, err)
	}
}
