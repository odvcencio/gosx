package server

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
)

func TestIslandEntryRegistration(t *testing.T) {
	p := &program.Program{Name: "Panels", Nodes: []program.Node{{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1}}, {Kind: program.NodeServerSlot, Tag: "div", Text: "Profile"}}}
	for _, format := range []string{"json", "bin"} {
		t.Run(format, func(t *testing.T) {
			r := NewPageRuntime()
			r.Island(&program.Program{Name: "Before", Nodes: []program.Node{{Kind: program.NodeElement, Tag: "p"}}}, nil)
			var data []byte
			var err error
			if format == "json" {
				data, err = program.EncodeJSON(p)
			} else {
				data, err = program.EncodeBinary(p)
			}
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			r.SetProgramAsset(p.Name, "/panels."+format, format, fmt.Sprintf("%x", sum[:8]))
			slots := island.ServerSlots{"Profile": gosx.El("input", gosx.Attrs(gosx.Attr("value", "kept")))}
			_, err = r.RenderIslandEntry(func(c *IslandRenderContext) gosx.Node {
				c.Island(p, nil, slots)
				return c.Island(p, nil, nil)
			})
			if err == nil || len(r.renderer.Manifest().Islands) != 1 {
				t.Fatalf("partial registration published: %v", err)
			}
			_, err = r.RenderIslandEntry(func(c *IslandRenderContext) gosx.Node {
				_, nested := r.RenderIslandEntry(func(*IslandRenderContext) gosx.Node { t.Fatal("reentrant callback ran"); return gosx.Node{} })
				if nested == nil || !strings.Contains(nested.Error(), "reentrant public") {
					t.Fatalf("nested: %v", nested)
				}
				return c.Island(p, nil, slots)
			})
			if err != nil || len(r.renderer.Manifest().Islands) != 2 {
				t.Fatalf("valid registration: %v", err)
			}
			r.SetProgramAsset(p.Name, "/stale", format, "bad")
			_, err = r.RenderIslandEntry(func(c *IslandRenderContext) gosx.Node { return c.Island(p, nil, slots) })
			if err == nil || !strings.Contains(err.Error(), "stale program") || len(r.renderer.Manifest().Islands) != 2 {
				t.Fatalf("stale asset: %v", err)
			}
		})
	}
	var absent *PageRuntime
	if _, err := absent.RenderIslandEntry(func(*IslandRenderContext) gosx.Node { return gosx.Node{} }); err == nil {
		t.Fatal("nil runtime accepted")
	}
}
