//go:build !tinygo && !js

package ir_test

import (
	"fmt"
	"m31labs.dev/gosx/ir"
	"strings"
	"testing"
)

func TestIslandAOTCheckerIndependentProfile(t *testing.T) {
	for _, tc := range []struct{ name, body, reason string }{
		{"reserved attribute", `count := signal.New(0); target := signal.New("change"); change := func(){count.Set(1)}; return <button data-gosx-on-click={target.Get()} onClick={change}>{count.Get()}</button>`, "reserved_attribute"},
		{"shared signal", `count := signal.NewShared("count", 0); return <div>{count.Get()}</div>`, "shared_signal_profile"},
		{"nonliteral shared key", `key := "count"; count := signal.NewShared(key, 0); return <div>{count.Get()}</div>`, "shared_signal_profile"},
		{"duplicate shared key", `first := signal.NewShared("count", 0); second := signal.NewShared("count", "x"); return <div>{first.Get()}{second.Get()}</div>`, "duplicate_signal_key"},
		{"expression depth", `count := signal.New(0); return <div>{` + strings.Repeat("(", 65) + "count.Get()" + strings.Repeat(" + 1)", 65) + `}</div>`, "graph_limit"},
		{"signal count", checkerSignals(17), "graph_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := checkerProgram(t, "", tc.body)
			_, err := ir.LowerIslandAOT(p, 0)
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("want %s, got %v", tc.reason, err)
			}
		})
	}
}

func checkerSignals(count int) string {
	var declarations, children strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&declarations, "s%d := signal.New(0)\n", i)
		fmt.Fprintf(&children, "{s%d.Get()}", i)
	}
	return declarations.String() + "return <div>" + children.String() + "</div>"
}

func TestIslandAOTCheckerLimitBoundaries(t *testing.T) {
	for _, depth := range []int{64, 65} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			p := checkerProgram(t, "", `count:=signal.New(0); return <div>{`+strings.Repeat("(", depth-1)+"count.Get()"+strings.Repeat("+1)", depth-1)+`}</div>`)
			_, err := ir.LowerIslandAOT(p, 0)
			if depth == 64 && err != nil {
				t.Fatal(err)
			}
			if depth == 65 && (err == nil || !strings.Contains(err.Error(), "graph_limit")) {
				t.Fatal(err)
			}
		})
	}
	p := checkerProgram(t, "", checkerSignals(16))
	if _, err := ir.LowerIslandAOT(p, 0); err != nil {
		t.Fatal(err)
	}
}

func TestIslandAOTDuplicateSignalKeysAreCompileErrors(t *testing.T) {
	p := checkerProgram(t, "", `a:=signal.NewShared("same",0); b:=signal.NewShared("same",1); return <div>{a.Get()}{b.Get()}</div>`)
	for _, diagnostic := range ir.Validate(p) {
		if diagnostic.Code == "duplicate_signal_key" {
			return
		}
	}
	t.Fatal("duplicate key passed general compilation validation")
}
