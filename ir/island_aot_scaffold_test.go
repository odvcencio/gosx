//go:build !tinygo && !js

package ir_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

func TestIslandAOTImplicitPackageBindings(t *testing.T) {
	for _, placement := range []string{"same file", "sibling.go", "sibling.gsx"} {
		for _, value := range []string{`"authored"`, "42"} {
			for _, onDisk := range []bool{false, true} {
				t.Run(placement+"/"+value+"/"+map[bool]string{false: "memory", true: "disk"}[onDisk], func(t *testing.T) {
					decl := "const value = " + value + "\n"
					candidate := "package example\n"
					if placement == "same file" {
						candidate += decl
					}
					candidate += "//gosx:island\ncomponent Counter(){\n text:=signal.New(\"\");change:=func(){text.Set(value)}\n return <input value={text} onInput={change}/>\n}\n"
					p := pairingSource(t, candidate)
					if onDisk {
						writeScaffoldSource(t, p.Dir, "counter.gsx", candidate)
					}
					if placement != "same file" {
						writeScaffoldSource(t, p.Dir, placement, "package example\n"+decl)
					}
					if _, err := ir.LowerIsland(p, 0); err != nil {
						t.Fatalf("VM path changed: %v", err)
					}
					_, err := ir.LowerIslandAOT(p, 0)
					if err == nil || !strings.Contains(err.Error(), "implicit_identifier_shadowed: value") {
						t.Fatalf("authored value binding lost: %v", err)
					}
				})
			}
		}
	}
}

func TestIslandAOTImplicitExcludedPackageBindings(t *testing.T) {
	p := checkerProgram(t, "", `text:=signal.New("");change:=func(){text.Set(value)};return <input value={text} onInput={change}/>`)
	for _, name := range []string{"excluded.go", "excluded.gsx", "excluded_test.go", "excluded_test.gsx"} {
		source := "package example\nconst value = 42\n"
		if !strings.Contains(name, "_test.") {
			source = "//go:build ignore\n\n" + source
		}
		writeScaffoldSource(t, p.Dir, name, source)
	}
	if _, err := ir.LowerIslandAOT(p, 0); err != nil {
		t.Fatal(err)
	}
}

func TestIslandAOTImplicitDeclarationKinds(t *testing.T) {
	for _, decl := range []string{
		`const (other, value = "other", "authored")`,
		`var other, value = "other", "authored"`,
		`func value() string {return "authored"}`,
		`type value = string`,
	} {
		t.Run(decl, func(t *testing.T) {
			p := checkerProgram(t, "", `text:=signal.New("");change:=func(){text.Set(value)};return <input onInput={change}/>`)
			writeScaffoldSource(t, p.Dir, "scope.go", "package example\n"+decl+"\n")
			if _, err := ir.LowerIslandAOT(p, 0); err == nil || !strings.Contains(err.Error(), "implicit_identifier_shadowed: value") {
				t.Fatal(err)
			}
		})
	}
	t.Run("receiver scope", func(t *testing.T) {
		p := checkerProgram(t, "", `text:=signal.New("");change:=func(){text.Set(value)};return <input onInput={change}/>`)
		writeScaffoldSource(t, p.Dir, "scope.go", "package example\ntype Other struct{}\nfunc (Other) value() string{return \"method\"}\n")
		if _, err := ir.LowerIslandAOT(p, 0); err != nil {
			t.Fatal(err)
		}
	})
}

func TestIslandAOTImplicitSiblingCache(t *testing.T) {
	p := checkerProgram(t, "", `text:=signal.New("");change:=func(){text.Set(value)};return <input onInput={change}/>`)
	before, err := ir.LowerIslandAOT(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	writeScaffoldSource(t, p.Dir, "scope.gsx", "package example\nconst value = 42\n")
	for _, cold := range []bool{false, true} {
		if cold {
			ir.AOTResetCheckCacheForTest()
		}
		if _, err := ir.LowerIslandAOT(p, 0); err == nil || !strings.Contains(err.Error(), "implicit_identifier_shadowed: value") {
			t.Fatalf("cold=%t: %v", cold, err)
		}
	}
	if err := os.Remove(filepath.Join(p.Dir, "scope.gsx")); err != nil {
		t.Fatal(err)
	}
	after, err := ir.LowerIslandAOT(p, 0)
	if err != nil || before.Digest != after.Digest {
		t.Fatalf("removing sibling changed original contract: %v", err)
	}
}

func TestIslandAOTImplicitFileImportBinding(t *testing.T) {
	p := pairingSource(t, "package example\nimport value \"m31labs.dev/gosx/signal\"\n//gosx:island\ncomponent Counter(){\n text:=signal.New(\"\");change:=func(){text.Set(value)}\n return <input onInput={change}/>\n}\n")
	if _, err := ir.LowerIslandAOT(p, 0); err == nil || !strings.Contains(err.Error(), "implicit_identifier_shadowed: value") {
		t.Fatalf("authored file import binding lost: %v", err)
	}
}

func writeScaffoldSource(t *testing.T, dir, name, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
}
