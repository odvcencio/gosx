package bridge

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	runtimewasm "m31labs.dev/gosx/client/runtime/wasm"
	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

func batchReaderProgram(fields int, observe bool) *program.Program {
	p := &program.Program{Name: "BatchReader"}
	zero := program.ExprID(fields)
	for i := range fields {
		name := fmt.Sprintf("$batch.%d", i)
		p.Signals = append(p.Signals, program.SignalDef{Name: name, Type: program.TypeInt, Init: zero})
		p.Exprs = append(p.Exprs, program.Expr{Op: program.OpSignalGet, Value: name, Type: program.TypeInt})
	}
	p.Exprs = append(p.Exprs, program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	if observe {
		p.Exprs = append(p.Exprs,
			program.Expr{Op: program.OpAdd, Operands: []program.ExprID{zero - 1, zero - 1}, Type: program.TypeInt},
			program.Expr{Op: program.OpSignalGet, Value: "twice", Type: program.TypeInt},
		)
		p.Computeds = []program.ComputedDef{{Name: "twice", Type: program.TypeInt, Expr: zero + 1}}
		args := make([]program.ExprID, fields+1)
		for i := range fields {
			args[i] = program.ExprID(i)
		}
		args[fields] = zero + 2
		p.Exprs = append(p.Exprs, program.Expr{Op: program.OpHostCall, Value: "probe.Read", Operands: args, Type: program.TypeAny})
		p.Nodes = []program.Node{{Kind: program.NodeExpr, Expr: zero + 3}}
		return p
	}
	p.Nodes = []program.Node{{Kind: program.NodeElement, Tag: "div"}}
	for i := range fields {
		index := program.NodeID(len(p.Nodes))
		p.Nodes[0].Children = append(p.Nodes[0].Children, index)
		p.Nodes = append(p.Nodes,
			program.Node{Kind: program.NodeElement, Tag: "span", Children: []program.NodeID{index + 1}},
			program.Node{Kind: program.NodeExpr, Expr: program.ExprID(i)},
		)
	}
	return p
}

func batchValues(fields, value int) map[string]vm.Value {
	values := make(map[string]vm.Value, fields)
	for i := range fields {
		values[fmt.Sprintf("$batch.%d", i)] = vm.IntVal(value)
	}
	return values
}

func batchJSON(fields, value int) string {
	values := make(map[string]int, fields)
	for i := range fields {
		values[fmt.Sprintf("$batch.%d", i)] = value
	}
	data, _ := json.Marshal(values)
	return string(data)
}

func hydrateBatchReader(t testing.TB, b *Bridge, id string, fields int, observe bool) {
	t.Helper()
	p := batchReaderProgram(fields, observe)
	data, err := program.EncodeJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.HydrateIsland(id, p.Name, `{}`, data, "json"); err != nil {
		t.Fatal(err)
	}
}

func TestSharedBatchReconcilesOnceAfterDerivedValuesSettle(t *testing.T) {
	for _, mode := range []string{"store", "events", "snapshot"} {
		t.Run(mode, func(t *testing.T) {
			b := New()
			recorders := map[string]*vm.HostRecorder{}
			b.RegisterIslandHostFactory("probe", func(id string) vm.HostReceiver {
				r := vm.NewHostRecorder()
				recorders[id] = r
				return r
			})
			for _, id := range []string{"first", "second"} {
				hydrateBatchReader(t, b, id, 32, true)
				recorders[id].Reset()
			}
			events := 0
			b.SetSharedSignalCallback(func(string, string) { events++ })
			for _, value := range []int{1, 1, 2, 2} {
				before := events
				switch mode {
				case "store":
					b.GetStore().SetBatch(batchValues(32, value))
				case "events":
					if err := b.SetSharedSignalBatchJSON(batchJSON(32, value)); err != nil {
						t.Fatal(err)
					}
				case "snapshot":
					if err := b.SetChangedSharedSignalBatchJSON(batchJSON(32, value)); err != nil {
						t.Fatal(err)
					}
				}
				want := 1
				if mode == "snapshot" && before == value*32 {
					want = 0
				}
				if events-before != want*32 {
					t.Fatalf("signal notifications = %d, want %d", events-before, want*32)
				}
				for id, r := range recorders {
					if len(r.Calls) != want {
						t.Errorf("%s: evaluations = %d, want %d", id, len(r.Calls), want)
					}
					for _, call := range r.Calls {
						for i, arg := range call.Args {
							expected := int64(value)
							if i == 32 {
								expected *= 2
							}
							if arg.Int64() != expected {
								t.Errorf("%s: input %d = %d, want %d", id, i, arg.Int64(), expected)
							}
						}
					}
					r.Reset()
				}
			}
		})
	}
}

func TestSharedBatchIncludesNestedObserverWrites(t *testing.T) {
	b := New()
	r := vm.NewHostRecorder()
	b.RegisterIslandHostFactory("probe", func(string) vm.HostReceiver { return r })
	hydrateBatchReader(t, b, "reader", 8, true)
	r.Reset()
	nested := false
	b.SetSharedSignalCallback(func(string, string) {
		if !nested {
			nested = true
			b.GetStore().SetBatch(map[string]vm.Value{"$batch.7": vm.IntVal(99)})
		}
	})
	b.GetStore().SetBatch(batchValues(8, 1))
	if len(r.Calls) != 1 || r.Calls[0].Args[7].Int64() != 99 || r.Calls[0].Args[8].Int64() != 198 {
		t.Fatalf("nested batch did not settle once: %+v", r.Calls)
	}
}

func TestSharedBatchSingleFieldComputedDoesNotLag(t *testing.T) {
	b := New()
	r := vm.NewHostRecorder()
	b.RegisterIslandHostFactory("probe", func(string) vm.HostReceiver { return r })
	hydrateBatchReader(t, b, "reader", 1, true)
	r.Reset()
	for value := 1; value <= 8; value++ {
		if err := b.SetSharedSignalBatchJSON(batchJSON(1, value)); err != nil {
			t.Fatal(err)
		}
		if len(r.Calls) != 1 || r.Calls[0].Args[0].Int64() != int64(value) || r.Calls[0].Args[1].Int64() != int64(2*value) {
			t.Fatalf("frame %d did not settle: %+v", value, r.Calls)
		}
		r.Reset()
	}
}

func TestSharedBatchOldSubscriptionsCannotRenderReplacement(t *testing.T) {
	for _, reload := range []bool{false, true} {
		t.Run(fmt.Sprint(reload), func(t *testing.T) {
			b := New()
			var readers []*vm.HostRecorder
			b.RegisterIslandHostFactory("probe", func(string) vm.HostReceiver {
				r := vm.NewHostRecorder()
				readers = append(readers, r)
				return r
			})
			p := batchReaderProgram(8, true)
			data, err := program.EncodeJSON(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := b.HydrateIsland("reader", p.Name, `{}`, data, "json"); err != nil {
				t.Fatal(err)
			}
			readers[0].Reset()
			notifications := 0
			b.SetSharedSignalCallback(func(string, string) {
				notifications++
				if notifications != 3 {
					return
				}
				if reload {
					err = b.ReloadProgram("reader", data, "json")
				} else {
					err = b.HydrateIsland("reader", p.Name, `{}`, data, "json")
				}
				if err != nil {
					t.Fatal(err)
				}
			})
			b.GetStore().SetBatch(batchValues(8, 1))
			if len(readers) != 2 || len(readers[0].Calls) != 0 || len(readers[1].Calls) != 1 {
				t.Fatalf("old notifications reached a retired or replacement reader: %+v", readers)
			}
			b.GetStore().SetBatch(batchValues(8, 2))
			if len(readers[1].Calls) != 2 || readers[1].Calls[1].Args[8].Int64() != 4 {
				t.Fatal("replacement reader did not receive the next batch")
			}
		})
	}
}

func TestSharedBatchAliasesShareOneSettledReader(t *testing.T) {
	b := New()
	r := vm.NewHostRecorder()
	b.RegisterIslandHostFactory("probe", func(string) vm.HostReceiver { return r })
	p := batchReaderProgram(2, true)
	for i, name := range []string{"$scene.event.worldX", "$surface.event.pointerY"} {
		p.Signals[i].Name = name
		p.Exprs[i].Value = name
	}
	data, err := program.EncodeJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.HydrateIsland("reader", p.Name, `{}`, data, "json"); err != nil {
		t.Fatal(err)
	}
	r.Reset()
	if err := b.SetSharedSignalBatchJSON(`{"$surface.event.worldX":7,"$scene.event.pointerY":9}`); err != nil {
		t.Fatal(err)
	}
	if len(r.Calls) != 1 {
		t.Fatalf("aliased reader evaluated %d times", len(r.Calls))
	}
	for i, expected := range []int64{7, 9, 18} {
		if got := r.Calls[0].Args[i].Int64(); got != expected {
			t.Fatalf("input %d = %d, want %d", i, got, expected)
		}
	}
}

func TestSharedBatchPatchesAreSettledAndAllowReentrantWrites(t *testing.T) {
	b := New()
	p := batchReaderProgram(2, true)
	// Read the first field plus twice the second through a derived value.
	p.Exprs = append(p.Exprs, program.Expr{Op: program.OpAdd, Operands: []program.ExprID{0, 4}, Type: program.TypeInt})
	p.Nodes = []program.Node{{Kind: program.NodeExpr, Expr: 6}}
	data, err := program.EncodeJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.HydrateIsland("reader", p.Name, `{}`, data, "json"); err != nil {
		t.Fatal(err)
	}
	if err := b.HydrateIsland("peer", p.Name, `{}`, data, "json"); err != nil {
		t.Fatal(err)
	}
	var texts []string
	var peerTexts []string
	b.SetPatchMailboxCallback(func(id string, mailbox []byte) {
		_, _, patches, err := runtimewasm.DecodePatchMailbox(mailbox)
		if err != nil || len(patches) != 1 {
			t.Fatalf("patches=%+v, error=%v", patches, err)
		}
		if id == "peer" {
			peerTexts = append(peerTexts, patches[0].Text)
			return
		}
		texts = append(texts, patches[0].Text)
		if len(texts) == 1 {
			b.GetStore().SetBatch(map[string]vm.Value{"$batch.0": vm.IntVal(3), "$batch.1": vm.IntVal(4)})
		}
	})
	b.GetStore().SetBatch(map[string]vm.Value{"$batch.0": vm.IntVal(1), "$batch.1": vm.IntVal(2)})
	if !reflect.DeepEqual(texts, []string{"5", "11"}) {
		t.Fatalf("batch patches = %q, want settled outer and nested states", texts)
	}
	if !reflect.DeepEqual(peerTexts, []string{"11"}) && !reflect.DeepEqual(peerTexts, []string{"5", "11"}) {
		t.Fatalf("peer patches = %q, want settled states in order", peerTexts)
	}
	if err := b.SetSharedSignalJSON("$batch.0", `7`); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(texts, []string{"5", "11", "15"}) {
		t.Fatalf("individual write was not immediate: %q", texts)
	}
}

func TestSharedBatchSkipsDisposedReadersAndRecoversAfterObserverPanic(t *testing.T) {
	for _, panicObserver := range []bool{false, true} {
		t.Run(fmt.Sprint(panicObserver), func(t *testing.T) {
			b := New()
			r := vm.NewHostRecorder()
			b.RegisterIslandHostFactory("probe", func(string) vm.HostReceiver { return r })
			hydrateBatchReader(t, b, "reader", 8, true)
			r.Reset()
			b.SetSharedSignalCallback(func(string, string) {
				if panicObserver {
					panic("observer")
				}
				b.DisposeIsland("reader")
			})
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				b.GetStore().SetBatch(batchValues(8, 1))
			}()
			if panicObserver && recovered != "observer" || !panicObserver && recovered != nil {
				t.Fatalf("observer panic = %v", recovered)
			}
			if !panicObserver && len(r.Calls) != 0 {
				t.Fatal("disposed reader was evaluated")
			}
			b.SetSharedSignalCallback(nil)
			if !panicObserver {
				hydrateBatchReader(t, b, "reader", 8, true)
			}
			r.Reset()
			b.GetStore().SetBatch(batchValues(8, 2))
			if len(r.Calls) != 1 || r.Calls[0].Args[8].Int64() != 4 {
				t.Fatalf("reader did not resume after callback: %+v", r.Calls)
			}
		})
	}
}

func BenchmarkSharedBatchReconcile(b *testing.B) {
	for _, fields := range []int{1, 8, 32, 64} {
		for _, readers := range []int{1, 4} {
			b.Run(fmt.Sprintf("fields=%d/readers=%d", fields, readers), func(b *testing.B) {
				bridge := New()
				bridge.SetSharedSignalCallback(func(string, string) {})
				for i := range readers {
					hydrateBatchReader(b, bridge, fmt.Sprint(i), fields, false)
				}
				payloads := []string{batchJSON(fields, 1), batchJSON(fields, 2)}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := bridge.SetSharedSignalBatchJSON(payloads[i&1]); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
