package bridge

import (
	"fmt"
	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
	"strings"
	"testing"
)

func TestServerSlotClosureCaptureRefusedBeforeEffects(t *testing.T) {
	b := New()
	created, patches := 0, 0
	b.RegisterIslandHostFactory("browser", func(string) vm.HostReceiver { created++; return vm.NewHostRecorder() })
	b.SetPatchCallback(func(string, string) { patches++ })
	b.store.Set("$tab", vm.ClosureVal("private", nil, nil))
	data, _ := program.EncodeJSON(serverSlotTestProgram(true))
	_, err := b.PrepareServerSlots("new", "Panels", `{}`, data, "json")
	if err == nil || !strings.Contains(err.Error(), "shared snapshot requires finite data, not closures") {
		t.Fatalf("wrong refusal: %v", err)
	}
	if created != 0 || patches != 0 || len(b.islands) != 0 || len(b.serverSlotStages) != 0 {
		t.Fatal("closure refusal acquired state or hosts")
	}
}

func TestServerSlotPendingDisposal(t *testing.T) {
	b := New()
	created, disposed := 0, 0
	b.RegisterIslandHostFactory("browser", func(string) vm.HostReceiver {
		created++
		return &disposableHostRecorder{HostRecorder: vm.NewHostRecorder(), disposed: &disposed}
	})
	data, _ := program.EncodeJSON(serverSlotTestProgram(true))
	prepared, err := b.PrepareServerSlots("owner", "Panels", `{}`, data, "json")
	if err != nil {
		t.Fatal(err)
	}
	b.DisposeIsland("owner")
	if created != 1 || disposed != 1 || len(b.serverSlotStages) != 0 {
		t.Fatal("pending resources retained", created, disposed)
	}
	if err := b.CommitServerSlots("owner", prepared.Token); err == nil {
		t.Fatal("disposed preparation committed")
	}
}

func TestServerSlotPendingReplacement(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			b := New()
			created, disposed := 0, 0
			b.RegisterIslandHostFactory("browser", func(string) vm.HostReceiver {
				created++
				return &disposableHostRecorder{HostRecorder: vm.NewHostRecorder(), disposed: &disposed}
			})
			plain, _ := program.EncodeJSON(serverSlotTestProgram(false))
			projected, _ := program.EncodeJSON(serverSlotTestProgram(true))
			if existing {
				if err := b.HydrateIsland("owner", "Panels", `{}`, plain, "json"); err != nil {
					t.Fatal(err)
				}
			}
			prepared, err := b.PrepareServerSlots("owner", "Panels", `{}`, projected, "json")
			if err != nil {
				t.Fatal(err)
			}
			if err := b.HydrateIsland("owner", "Panels", `{}`, plain, "json"); err != nil {
				t.Fatal(err)
			}
			replacement := b.islands["owner"]
			if err := b.CommitServerSlots("owner", prepared.Token); err == nil {
				t.Fatal("replacement overwritten")
			}
			if b.islands["owner"] != replacement || created-disposed != 1 || len(b.serverSlotStages) != 0 {
				t.Fatal("replacement cleanup mismatch", created, disposed)
			}
		})
	}
}

func serverSlotTestProgram(projected bool) *program.Program {
	p := &program.Program{Name: "Panels", Nodes: []program.Node{{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1}}, {Kind: program.NodeElement, Tag: "div"}},
		Exprs:    []program.Expr{{Op: program.OpLitString, Value: "profile", Type: program.TypeString}, {Op: program.OpSignalGet, Value: "$tab", Type: program.TypeAny}, {Op: program.OpNeq, Operands: []program.ExprID{1, 0}, Type: program.TypeBool}, {Op: program.OpHostCall, Value: "browser.Focus", Type: program.TypeAny}},
		Signals:  []program.SignalDef{{Name: "$tab", Type: program.TypeString, Init: 0}},
		Handlers: []program.Handler{{Name: "focus", Body: []program.ExprID{3}}}}
	if projected {
		p.Nodes[1] = program.Node{Kind: program.NodeServerSlot, Tag: "div", Text: "Profile", Attrs: []program.Attr{{Kind: program.AttrExpr, Name: "hidden", Expr: 2}}}
	}
	return p
}

func TestServerSlotStagedOwnership(t *testing.T) {
	b := New()
	created, disposed, patches := 0, 0, 0
	b.RegisterIslandHostFactory("browser", func(string) vm.HostReceiver {
		created++
		return &disposableHostRecorder{HostRecorder: vm.NewHostRecorder(), disposed: &disposed}
	})
	plain, _ := program.EncodeJSON(serverSlotTestProgram(false))
	for _, id := range []string{"owner", "sibling"} {
		if err := b.HydrateIsland(id, "Panels", `{}`, plain, "json"); err != nil {
			t.Fatal(err)
		}
	}
	old, sibling := b.islands["owner"], b.islands["sibling"]
	b.SetPatchCallback(func(string, string) { patches++ })
	data, err := program.EncodeJSON(serverSlotTestProgram(true))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := b.PrepareServerSlots("owner", "Panels", `{}`, data, "json")
	if err != nil {
		t.Fatal(err)
	}
	if patches != 0 || b.islands["owner"] != old || b.islands["sibling"] != sibling || disposed != 0 {
		t.Fatal("prepare affected live owner")
	}
	if err := b.AbortServerSlots("owner", prepared.Token); err != nil {
		t.Fatal(err)
	}
	if disposed != 1 || created != 3 || b.islands["owner"] != old {
		t.Fatalf("abort ownership created%d disposed%d", created, disposed)
	}
	prepared, err = b.PrepareServerSlots("owner", "Panels", `{}`, data, "json")
	if err != nil {
		t.Fatal(err)
	}
	b.store.Set("$unrelated", vm.StringVal("changed"))
	if err := b.CommitServerSlots("owner", prepared.Token); err != nil {
		t.Fatal(err)
	}
	if b.islands["owner"] == old || b.islands["sibling"] != sibling || disposed != 2 {
		t.Fatal("commit ownership wrong")
	}
	value := vm.StringVal("security")
	b.store.Set("$tab", value)
	if !b.islands["owner"].CurrentTree().Nodes[1].Attrs[0].Bool {
		t.Fatal("committed owner not rebound to live store")
	}
	current := b.islands["owner"]
	prepared, err = b.PrepareServerSlots("owner", "Panels", `{}`, data, "json")
	if err != nil {
		t.Fatal(err)
	}
	b.store.Set("$tab", vm.StringVal("profile"))
	if err := b.CommitServerSlots("owner", prepared.Token); err == nil {
		t.Fatal("stale shared capture committed")
	}
	if b.islands["owner"] != current {
		t.Fatal("stale commit disposed old owner")
	}
	if err := b.AbortServerSlots("owner", prepared.Token); err != nil {
		t.Fatal(err)
	}
}

func TestServerSlotAbsentSharedIsPrivate(t *testing.T) {
	b := New()
	data, _ := program.EncodeJSON(serverSlotTestProgram(true))
	prepared, err := b.PrepareServerSlots("new", "Panels", `{}`, data, "json")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := b.store.Get("$tab"); exists {
		t.Fatal("preparation published absent shared signal")
	}
	if err := b.AbortServerSlots("new", prepared.Token); err != nil {
		t.Fatal(err)
	}
	if _, exists := b.store.Get("$tab"); exists {
		t.Fatal("abort published shared signal")
	}
	prepared, err = b.PrepareServerSlots("new", "Panels", `{}`, data, "json")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.CommitServerSlots("new", prepared.Token); err != nil {
		t.Fatal(err)
	}
	if value, exists := b.store.Get("$tab"); !exists || value.String() != "profile" {
		t.Fatal("commit did not initialize missing shared signal")
	}
}
