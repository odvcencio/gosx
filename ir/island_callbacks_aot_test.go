//go:build !tinygo && !js

package ir_test

import (
	"bytes"
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

func TestAuthoredIslandCallbackAOT(t *testing.T) {
	p, err := parseAOT(t, []byte(callbackCounterSource))
	if err != nil {
		t.Fatal(err)
	}
	p.PackagePath = "example/components"
	for index, component := range p.Components {
		if component.Name != "Counter" {
			continue
		}
		vmProgram, err := ir.LowerIsland(p, index)
		if err != nil {
			t.Fatal(err)
		}
		want, err := program.EncodeBinary(vmProgram)
		if err != nil {
			t.Fatal(err)
		}
		unit, err := ir.LowerIslandAOT(p, index)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(unit.ProgramBytes, want) {
			t.Fatal("AOT evidence changed the VM program")
		}
		return
	}
	t.Fatal("missing Counter")
}

func TestIslandCallbackAOTRejectsAlteredBindings(t *testing.T) {
	source := strings.Replace(callbackCounterSource, "OnActivate func()", "OnActivate func(); Other func()", 1)
	source = strings.Replace(source, "increment := func() { count.Set(count.Get() + 1) }", "increment := func() { count.Set(count.Get() + 1) }; decrement := func() { count.Set(count.Get() - 1) }", 1)
	source = strings.Replace(source, "return <Control Count={props.Count} OnActivate={props.OnActivate} />", "return <section><Control Count={props.Count} OnActivate={props.OnActivate} /><Control Count={props.Count} OnActivate={props.Other} /></section>", 1)
	source = strings.Replace(source, "<Wrapper Count={count.Get()} OnActivate={increment} />", "<Wrapper Count={count.Get()} OnActivate={increment} Other={decrement} />", 1)
	for _, mutation := range []string{"handler object", "forwarded field", "prop assignment"} {
		t.Run(mutation, func(t *testing.T) {
			p, err := parseAOT(t, []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			p.PackagePath = "example/components"
			index := -1
			for i, component := range p.Components {
				if component.Name == "Counter" {
					index = i
				}
			}
			if _, err := ir.LowerIslandAOT(p, index); err != nil {
				t.Fatalf("unmodified witness rejected: %v", err)
			}
			for nodeIndex := range p.Nodes {
				node := &p.Nodes[nodeIndex]
				if node.Tag == "Wrapper" {
					for i := range node.Attrs {
						attr := &node.Attrs[i]
						if mutation == "handler object" && attr.Name == "OnActivate" {
							attr.Expr = "decrement"
						}
						if mutation == "prop assignment" {
							if attr.Name == "OnActivate" {
								attr.Name = "Other"
							} else if attr.Name == "Other" {
								attr.Name = "OnActivate"
							}
						}
					}
				}
				if mutation == "forwarded field" && node.Tag == "Control" {
					for i := range node.Attrs {
						if node.Attrs[i].Expr == "props.OnActivate" {
							node.Attrs[i].Expr = "props.Other"
						}
					}
				}
			}
			if _, err := ir.LowerIsland(p, index); err != nil {
				t.Fatalf("mutation is not a valid VM witness: %v", err)
			}
			if _, err := ir.LowerIslandAOT(p, index); err == nil || !strings.Contains(err.Error(), "evidence_binding_mismatch") {
				t.Fatalf("altered callback admitted: %v", err)
			}
		})
	}
}
