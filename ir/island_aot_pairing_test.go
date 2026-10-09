//go:build !tinygo && !js

package ir_test

import (
	"bytes"
	"context"
	"fmt"
	"m31labs.dev/gosx/island/program"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/ir"
)

func TestIslandAOTGoSXPairing(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"counter", `count := signal.New(0); change := func(){count.Set(count.Get()+1)}; return <button type="button" onClick={change}>{count.Get()}</button>`},
		{"bare signal", `count := signal.New(0); return <div title={count}>{count}</div>`},
		{"computed receiver", `count := signal.New(0); doubled := signal.Derive(func() int { return count.Get()+count.Get() }); return <div>{doubled.Get()}</div>`},
		{"unused computed", `count := signal.New(0); doubled := signal.Derive(func() int { return count.Get()+count.Get() }); return <div>{count}</div>`},
		{"ternary", `return <div>{true ? 1 : 2}</div>`},
		{"nested ternary", `return <div>{true ? (false ? 1 : 2) : 3}</div>`},
		{"typed ternary", `count := signal.New[int32](0); return <div>{true ? count.Get() : 2}</div>`},
		{"implicit value", `text := signal.New(""); change := func(){text.Set(value)}; return <input value={text.Get()} onInput={change}/>`},
		{"inline handler", `count := signal.New(0); return <button type="button" data-on-click="count.Set(1)">{count.Get()}</button>`},
		{"inline sequence", `count := signal.New(0); return <button type="button" data-on-click="count.Set(1); count.Set(count.Get()+1)">{count.Get()}</button>`},
		{"handler ternary", `count := signal.New(0); change:=func(){count.Set(true ? 1 : 2)};return <button onClick={change}>{count}</button>`},
		{"computed bare", `doubled:=signal.Derive(func() int32 {return 2});return <div>{doubled}</div>`},
		{"event bool", `flag := signal.New(false); change := func(){flag.Set(checked)}; return <input onInput={change} checked={flag}/>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := checkerProgram(t, "", tc.body)
			if errors := ir.AOTCheckingHardErrorsForTest(p); len(errors) > 0 {
				t.Fatal("scaffold hard errors", errors)
			}
			before, err := ir.LowerIsland(p, 0)
			if err != nil {
				t.Fatal(err)
			}
			u, err := ir.LowerIslandAOT(p, 0)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := program.EncodeBinary(before)
			b := u.ProgramBytes
			if !bytes.Equal(a, b) {
				t.Fatal("pairing changed the VM artifact")
			}
		})
	}
}

func TestIslandAOTAutoReadComponentProp(t *testing.T) {
	p := pairingSource(t, "package example\ntype ChildProps struct{Number int}\ncomponent Child(props: ChildProps) {\n return <span>{props.Number}</span>\n}\n//gosx:island\ncomponent Counter(){\n count := signal.New(0)\n return <div><Child Number={count}/></div>\n}\n")
	if hard := ir.AOTCheckingHardErrorsForTest(p); len(hard) != 0 {
		t.Fatal(hard)
	}
	if _, err := ir.LowerIslandAOT(p, 1); err != nil {
		t.Fatal(err)
	}
}

func TestIslandAOTScaffoldPropSpreads(t *testing.T) {
	for _, tc := range []struct{ name, param, view string }{
		{"data", "", `<Counter {...data.Counter}/>`},
		{"props", "props CounterProps", `<Counter {...props}/>`},
		{"field", "props PageProps", `<Counter {...props.Counter}/>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "package example\ntype CounterProps struct{Initial int}\ntype PageProps struct{Counter CounterProps}\nvar data struct{Counter CounterProps}\n//gosx:island\ncomponent Counter(props: CounterProps){\n count:=signal.New(props.Initial);change:=func(){count.Set(count.Get()+1)}\n return <button onClick={change}>{count.Get()}</button>\n}\nfunc Page(" + tc.param + ") Node {\n return " + tc.view + "\n}\n"
			p := pairingSource(t, source)
			if hard := ir.AOTCheckingHardErrorsForTest(p); len(hard) != 0 {
				t.Fatal(hard)
			}
			before, err := ir.LowerIsland(p, 0)
			if err != nil {
				t.Fatal(err)
			}
			u, err := ir.LowerIslandAOT(p, 0)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := program.EncodeBinary(before)
			if !bytes.Equal(encoded, u.ProgramBytes) {
				t.Fatal("server prop spread changed the island VM artifact")
			}
		})
	}
}

func TestIslandAOTGoSXPairingRejections(t *testing.T) {
	for _, tc := range []struct{ name, decl, body, reason string }{
		{"mixed ternary", "", `return <div>{true ? 1 : "x"}</div>`, "conditional_type_mismatch"},
		{"wrong Set type", "", `count := signal.New(0); change := func(){count.Set("x")}; return <button onClick={change}>{count}</button>`, "type_error"},
		{"foreign Get", `type Other struct{}; func (Other) Get() int { return 0 }`, `count := Other{}; return <div>{count.Get()}</div>`, "unknown method"},
		{"shadowed value", `const value = "shadow"`, `text := signal.New(""); change := func(){text.Set(value)}; return <input onInput={change} value={text}/>`, "evidence_binding_mismatch"},
		{"event type shadow", `type string = int`, `text:=signal.New(0);change:=func(){text.Set(value)};return <input onInput={change} value={text}/>`, "evidence_binding_mismatch"},
		{"scaffold helper shadow", `func __gosx_aot_choose(c bool,a,b int32) int32 {return b}`, `return <div>{true ? 1 : 2}</div>`, "evidence_shape_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ir.LowerIslandAOT(checkerProgram(t, tc.decl, tc.body), 0)
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("want %s, got %v", tc.reason, err)
			}
		})
	}
}

func TestIslandAOTCacheAuthoredSpans(t *testing.T) {
	source := "package example\ntype NumberProps struct{Value int32}\ntype TextProps struct{Value string}\n//gosx:island\nfunc Counter(props NumberProps) Node {\n return <div>{props.Value}</div>\n}\nfunc Other(props TextProps) Node {\n return <div>{props.Value}</div>\n}\n"
	p := pairingSource(t, source)
	if _, err := ir.LowerIslandAOT(p, 0); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []string{
		strings.Replace(source, "<div>{props.Value}", "<div>\n{props.Value}", 1),
		strings.Replace(source, "NumberProps) Node", "TextProps) Node", 1),
	} {
		warm := pairingSource(t, edit)
		warm.Dir = p.Dir
		cold := *warm

		a, err := ir.LowerIslandAOT(warm, 0)
		if err != nil {
			t.Fatal(err)
		}
		ir.AOTResetCheckCacheForTest()
		b, err := ir.LowerIslandAOT(&cold, 0)
		if err != nil {
			t.Fatal(err)
		}
		if a.Digest != b.Digest {
			t.Fatal("warm cache changed scalar evidence")
		}
	}
}

func TestIslandAOTRepeatedCompositionBound(t *testing.T) {
	if os.Getenv("GOSX_TEST_AOT_COMPOSITION") == "1" {
		var source strings.Builder
		source.WriteString("package example\n//gosx:island\n")
		for i := 0; i <= 30; i++ {
			fmt.Fprintf(&source, "component C%d() {\n return ", i)
			if i == 30 {
				source.WriteString("<span>leaf</span>\n}\n")
			} else {
				fmt.Fprintf(&source, "<div><C%d/><C%d/></div>\n}\n", i+1, i+1)
			}
		}
		p := pairingSource(t, source.String())
		start := time.Now()
		_, err := ir.LowerIslandAOT(p, 0)
		if err == nil || !strings.Contains(err.Error(), "65,535 expanded-node limit") {
			t.Fatalf("expansion cap: %v", err)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("repeated traversal took %v", elapsed)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestIslandAOTRepeatedCompositionBound$")
	command.Env = append(os.Environ(), "GOSX_TEST_AOT_COMPOSITION=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bounded admission: %v %s", err, output)
	}
}

func pairingSource(t *testing.T, source string) *ir.Program {
	t.Helper()
	p, err := parseAOT(t, []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	p.PackagePath = "example/components"
	return p
}

func TestIslandAOTPackageEvidenceSharing(t *testing.T) {
	dir := t.TempDir()
	sourceA := "package example\n//gosx:island\nfunc First() Node {\n return <div>{true ? 1 : 2}</div>\n}\n"
	sourceB := "package example\n//gosx:island\nfunc Second() Node {\n return <div>{true ? 2 : 3}</div>\n}\n"
	a, b := pairingSource(t, sourceA), pairingSource(t, sourceB)
	a.Dir, b.Dir = dir, dir
	for name, source := range map[string]string{"first.gsx": sourceA, "second.gsx": sourceB} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if !ir.AOTPackageEvidenceSharedForTest(a, b) {
		t.Fatal("package candidates did not share checked AST objects")
	}
	for _, p := range []*ir.Program{a, b} {
		if _, err := ir.LowerIslandAOT(p, 0); err != nil {
			t.Fatal(err)
		}
	}
}
