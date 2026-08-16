package main

import (
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
			wantErr:        "gosx v0.38.1 cannot operate on a project pinned to m31labs.dev/gosx v0.31.4. Run: go install m31labs.dev/gosx/cmd/gosx@v0.31.4",
		},
		{
			name:           "newer project also fails",
			cliVersion:     "v0.31.4",
			projectVersion: "v0.38.1",
			wantErr:        "gosx v0.31.4 cannot operate on a project pinned to m31labs.dev/gosx v0.38.1. Run: go install m31labs.dev/gosx/cmd/gosx@v0.38.1",
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

func TestParseGoSXRequirement(t *testing.T) {
	cases := []struct {
		name             string
		goMod            string
		wantVersion      string
		wantLocalReplace bool
	}{
		{
			name: "single-line require",
			goMod: `module example.com/app

go 1.26

require m31labs.dev/gosx v0.31.4
`,
			wantVersion: "v0.31.4",
		},
		{
			name: "require block",
			goMod: `module example.com/app

go 1.26

require (
	m31labs.dev/gosx v0.31.4
	github.com/other/thing v1.0.0
)
`,
			wantVersion: "v0.31.4",
		},
		{
			name: "no gosx dependency",
			goMod: `module example.com/app

go 1.26

require github.com/other/thing v1.0.0
`,
			wantVersion: "",
		},
		{
			name: "relative local replace",
			goMod: `module example.com/app

go 1.26

require m31labs.dev/gosx v0.31.4

replace m31labs.dev/gosx => ../../gosx
`,
			wantVersion:      "v0.31.4",
			wantLocalReplace: true,
		},
		{
			name: "absolute local replace",
			goMod: `module example.com/app

go 1.26

require m31labs.dev/gosx v0.31.4

replace m31labs.dev/gosx => /home/dev/gosx
`,
			wantVersion:      "v0.31.4",
			wantLocalReplace: true,
		},
		{
			name: "replace to another module version is not local",
			goMod: `module example.com/app

go 1.26

require m31labs.dev/gosx v0.31.4

replace m31labs.dev/gosx => example.com/fork v0.31.5
`,
			wantVersion:      "v0.31.4",
			wantLocalReplace: false,
		},
		{
			name: "replace with an old-version constraint stays local",
			goMod: `module example.com/app

go 1.26

require m31labs.dev/gosx v0.31.4

replace m31labs.dev/gosx v0.31.4 => ../local/gosx
`,
			wantVersion:      "v0.31.4",
			wantLocalReplace: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			version, hasLocalReplace := parseGoSXRequirement(tc.goMod)
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q", version, tc.wantVersion)
			}
			if hasLocalReplace != tc.wantLocalReplace {
				t.Errorf("hasLocalReplace = %v, want %v", hasLocalReplace, tc.wantLocalReplace)
			}
		})
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
