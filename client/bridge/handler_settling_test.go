package bridge

import (
	"fmt"
	"strings"
	"testing"

	runtimewasm "m31labs.dev/gosx/client/runtime/wasm"
	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

func sharedWriterProgram(fields int) *program.Program {
	p := batchReaderProgram(fields, false)
	p.Name = "SharedWriter"
	one := program.ExprID(len(p.Exprs))
	p.Exprs = append(p.Exprs, program.Expr{Op: program.OpLitInt, Value: "1", Type: program.TypeInt})
	advance := program.Handler{Name: "advance"}
	pulse := program.Handler{Name: "pulse"}
	for i := range fields {
		add := program.ExprID(len(p.Exprs))
		p.Exprs = append(p.Exprs,
			program.Expr{Op: program.OpAdd, Operands: []program.ExprID{program.ExprID(i), one}, Type: program.TypeInt},
			program.Expr{Op: program.OpSignalSet, Value: p.Signals[i].Name, Operands: []program.ExprID{add}, Type: program.TypeInt},
			program.Expr{Op: program.OpSignalSet, Value: p.Signals[i].Name, Operands: []program.ExprID{program.ExprID(i)}, Type: program.TypeInt},
		)
		advance.Body = append(advance.Body, add+1)
		pulse.Body = append(pulse.Body, add+2)
	}
	p.Handlers = []program.Handler{advance, pulse}
	return p
}

func derivedSharedReaderProgram(fields int) *program.Program {
	p := batchReaderProgram(fields, false)
	start := program.ExprID(len(p.Exprs))
	p.Exprs = append(p.Exprs,
		program.Expr{Op: program.OpAdd, Operands: []program.ExprID{program.ExprID(fields - 1), program.ExprID(fields - 1)}, Type: program.TypeInt},
		program.Expr{Op: program.OpSignalGet, Value: "twice", Type: program.TypeInt},
		program.Expr{Op: program.OpAdd, Operands: []program.ExprID{0, start + 1}, Type: program.TypeInt},
	)
	p.Computeds = []program.ComputedDef{{Name: "twice", Expr: start, Type: program.TypeInt}}
	p.Nodes = []program.Node{{Kind: program.NodeExpr, Expr: start + 2}}
	return p
}

func TestSharedHandlerPeersReceiveOnlySettledDerivedPatches(t *testing.T) {
	for _, fields := range []int{1, 32} {
		t.Run(fmt.Sprint(fields), func(t *testing.T) {
			b := New()
			writer := sharedWriterProgram(fields)
			reader := derivedSharedReaderProgram(fields)
			if err := b.HydrateIsland("writer", writer.Name, `{}`, encodeDispatchProgram(t, writer), "json"); err != nil {
				t.Fatal(err)
			}
			if err := b.HydrateIsland("peer", reader.Name, `{}`, encodeDispatchProgram(t, reader), "json"); err != nil {
				t.Fatal(err)
			}
			var texts []string
			events := 0
			b.SetSharedSignalCallback(func(string, string) { events++ })
			b.SetPatchMailboxCallback(func(id string, data []byte) {
				_, _, patches, err := runtimewasm.DecodePatchMailbox(data)
				if err != nil || id != "peer" || len(patches) != 1 || patches[0].Kind != vm.PatchSetText {
					t.Fatalf("unexpected peer mailbox: id=%s patches=%+v error=%v", id, patches, err)
				}
				texts = append(texts, patches[0].Text)
			})
			for value := 1; value <= 8; value++ {
				patches, err := b.DispatchAction("writer", "advance", `{}`)
				if err != nil || len(patches) != fields {
					t.Fatalf("writer result: patches=%+v error=%v", patches, err)
				}
				if len(texts) != value || texts[value-1] != fmt.Sprint(3*value) {
					t.Fatalf("handler %d produced partial or stale peer states: %q", value, texts)
				}
				if events != fields*value {
					t.Fatalf("event count = %d, want %d", events, fields*value)
				}
			}
			patches, err := b.DispatchAction("writer", "pulse", `{}`)
			if err != nil || len(patches) != 0 || len(texts) != 8 || events != 9*fields {
				t.Fatalf("pulse semantics changed: patches=%+v texts=%q events=%d error=%v", patches, texts, events, err)
			}
		})
	}
}

func TestSharedHandlerPeerEvaluatesOncePerDispatch(t *testing.T) {
	b := New()
	r := vm.NewHostRecorder()
	b.RegisterIslandHostFactory("probe", func(string) vm.HostReceiver { return r })
	writer := sharedWriterProgram(32)
	if err := b.HydrateIsland("writer", writer.Name, `{}`, encodeDispatchProgram(t, writer), "json"); err != nil {
		t.Fatal(err)
	}
	hydrateBatchReader(t, b, "peer", 32, true)
	r.Reset()
	if _, err := b.DispatchAction("writer", "advance", `{}`); err != nil {
		t.Fatal(err)
	}
	if len(r.Calls) != 1 {
		t.Fatalf("peer evaluated %d times, want one settled evaluation", len(r.Calls))
	}
	for i, arg := range r.Calls[0].Args {
		want := int64(1)
		if i == 32 {
			want = 2
		}
		if arg.Int64() != want {
			t.Fatalf("peer input %d = %d, want %d", i, arg.Int64(), want)
		}
	}
}

func TestSharedHandlerKeepsWriterGuardDuringPeerCallbacks(t *testing.T) {
	b := New()
	writer := sharedWriterProgram(1)
	reader := derivedSharedReaderProgram(1)
	for id, p := range map[string]*program.Program{"writer": writer, "peer": reader} {
		if err := b.HydrateIsland(id, p.Name, `{}`, encodeDispatchProgram(t, p), "json"); err != nil {
			t.Fatal(err)
		}
	}
	called := false
	var nestedErr error
	b.SetPatchMailboxCallback(func(id string, _ []byte) {
		if id == "peer" && !called {
			called = true
			_, nestedErr = b.DispatchAction("writer", "advance", `{}`)
		}
	})
	if _, err := b.DispatchAction("writer", "advance", `{}`); err != nil {
		t.Fatal(err)
	}
	if !called || nestedErr == nil || !strings.Contains(nestedErr.Error(), "already dispatching") {
		t.Fatalf("writer guard ended before peer delivery: called=%v error=%v", called, nestedErr)
	}
}

func TestSharedHandlerOwnResultIncludesPeerObserverWrites(t *testing.T) {
	b := New()
	writer := sharedWriterProgram(1)
	reader := derivedSharedReaderProgram(1)
	for id, p := range map[string]*program.Program{"writer": writer, "peer": reader} {
		if err := b.HydrateIsland(id, p.Name, `{}`, encodeDispatchProgram(t, p), "json"); err != nil {
			t.Fatal(err)
		}
	}
	changed := false
	var ids []string
	b.SetPatchMailboxCallback(func(id string, _ []byte) {
		ids = append(ids, id)
		if id == "peer" && !changed {
			changed = true
			b.GetStore().Set("$batch.0", vm.IntVal(2))
		}
	})
	patches, err := b.DispatchAction("writer", "advance", `{}`)
	if err != nil || len(patches) != 1 || patches[0].Text != "2" {
		t.Fatalf("owned patches missed the peer callback's write: %+v (%v)", patches, err)
	}
	for _, id := range ids {
		if id != "peer" {
			t.Fatalf("writer patches were pushed instead of returned: %q", ids)
		}
	}
}

func TestSharedHandlerNestedActionReturnsCurrentDerivedValue(t *testing.T) {
	b := New()
	var inner []vm.PatchOp
	b.RegisterIslandHostFactory("nested", func(string) vm.HostReceiver {
		return &bridgeDispatchHost{call: func() error {
			var err error
			inner, err = b.DispatchAction("inner", "advance", `{}`)
			return err
		}}
	})
	outer := &program.Program{
		Name: "Outer", Exprs: []program.Expr{{Op: program.OpHostCall, Value: "nested.Advance", Type: program.TypeAny}},
		Handlers: []program.Handler{{Name: "advance", Body: []program.ExprID{0}}},
		Nodes:    []program.Node{{Kind: program.NodeElement, Tag: "div"}},
	}
	writer := sharedWriterProgram(1)
	start := program.ExprID(len(writer.Exprs))
	writer.Exprs = append(writer.Exprs,
		program.Expr{Op: program.OpAdd, Operands: []program.ExprID{0, 0}, Type: program.TypeInt},
		program.Expr{Op: program.OpSignalGet, Value: "twice", Type: program.TypeInt},
		program.Expr{Op: program.OpAdd, Operands: []program.ExprID{0, start + 1}, Type: program.TypeInt},
	)
	writer.Computeds = []program.ComputedDef{{Name: "twice", Expr: start, Type: program.TypeInt}}
	writer.Nodes = []program.Node{{Kind: program.NodeExpr, Expr: start + 2}}
	for id, p := range map[string]*program.Program{"outer": outer, "inner": writer} {
		if err := b.HydrateIsland(id, p.Name, `{}`, encodeDispatchProgram(t, p), "json"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.DispatchAction("outer", "advance", `{}`); err != nil {
		t.Fatal(err)
	}
	if len(inner) != 1 || inner[0].Text != "3" {
		t.Fatalf("nested action returned stale derived patches: %+v", inner)
	}
}

func TestSharedHandlerRecoversAfterUnknownHandlerAndObserverPanic(t *testing.T) {
	b := New()
	writer := sharedWriterProgram(1)
	reader := derivedSharedReaderProgram(1)
	for id, p := range map[string]*program.Program{"writer": writer, "peer": reader} {
		if err := b.HydrateIsland(id, p.Name, `{}`, encodeDispatchProgram(t, p), "json"); err != nil {
			t.Fatal(err)
		}
	}
	if patches, err := b.DispatchAction("writer", "unknown", `{}`); err != nil || len(patches) != 0 {
		t.Fatalf("unknown handler result: %+v (%v)", patches, err)
	}
	if b.store.updating || len(b.dispatching) != 0 {
		t.Fatal("unknown handler left an active update")
	}
	b.SetPatchMailboxCallback(func(string, []byte) { panic("peer") })
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = b.DispatchAction("writer", "advance", `{}`)
	}()
	if recovered != "peer" || b.store.updating || len(b.dispatching) != 0 {
		t.Fatalf("observer panic left an active update: panic=%v updating=%v dispatching=%v", recovered, b.store.updating, b.dispatching)
	}
	var text string
	b.SetPatchMailboxCallback(func(_ string, data []byte) {
		_, _, patches, err := runtimewasm.DecodePatchMailbox(data)
		if err != nil || len(patches) != 1 {
			t.Fatalf("invalid recovery mailbox: %+v (%v)", patches, err)
		}
		text = patches[0].Text
	})
	if patches, err := b.DispatchAction("writer", "advance", `{}`); err != nil || len(patches) != 1 || patches[0].Text != "2" || text != "6" {
		t.Fatalf("dispatch did not recover without remounting: %+v (%v), peer=%q", patches, err, text)
	}
}

func TestSharedHandlerKeepsWriterGuardDuringPanicCleanup(t *testing.T) {
	for _, panicInFlush := range []bool{false, true} {
		t.Run(fmt.Sprint(panicInFlush), func(t *testing.T) {
			b := New()
			writer := sharedWriterProgram(2)
			if err := b.HydrateIsland("writer", writer.Name, `{}`, encodeDispatchProgram(t, writer), "json"); err != nil {
				t.Fatal(err)
			}
			hydrateBatchReader(t, b, "peer", 2, false)
			b.SetSharedSignalCallback(func(name, _ string) {
				if name == "$batch.1" {
					panic("signal observer")
				}
			})
			called := false
			var nestedErr error
			b.SetPatchMailboxCallback(func(id string, _ []byte) {
				if id == "peer" {
					called = true
					_, nestedErr = b.DispatchAction("writer", "unknown", `{}`)
					if panicInFlush {
						panic("flush observer")
					}
				}
			})
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				_, _ = b.DispatchAction("writer", "advance", `{}`)
			}()
			wantPanic := "signal observer"
			if panicInFlush {
				wantPanic = "flush observer"
			}
			if recovered != wantPanic || !called || nestedErr == nil || !strings.Contains(nestedErr.Error(), "already dispatching") {
				t.Fatalf("cleanup lost the writer guard: panic=%v peer=%v nested=%v", recovered, called, nestedErr)
			}
			if b.store.updating || len(b.dispatching) != 0 {
				t.Fatal("panic cleanup left an active update")
			}
			b.SetSharedSignalCallback(nil)
			b.SetPatchMailboxCallback(nil)
			if _, err := b.DispatchAction("writer", "advance", `{}`); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func BenchmarkSharedHandlerDispatch(b *testing.B) {
	for _, fields := range []int{1, 8, 32} {
		for _, peers := range []int{1, 4} {
			b.Run(fmt.Sprintf("fields=%d/peers=%d", fields, peers), func(b *testing.B) {
				bridge := New()
				writer := sharedWriterProgram(fields)
				data, err := program.EncodeJSON(writer)
				if err != nil {
					b.Fatal(err)
				}
				if err := bridge.HydrateIsland("writer", writer.Name, `{}`, data, "json"); err != nil {
					b.Fatal(err)
				}
				for peer := range peers {
					hydrateBatchReader(b, bridge, fmt.Sprint(peer), fields, false)
				}
				bridge.SetSharedSignalCallback(func(string, string) {})
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := bridge.DispatchAction("writer", "advance", `{}`); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
