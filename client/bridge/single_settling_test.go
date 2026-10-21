package bridge

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	runtimewasm "m31labs.dev/gosx/client/runtime/wasm"
	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

func TestSharedSingleWriteSettlesDerivedValues(t *testing.T) {
	for _, mode := range []string{"store", "json"} {
		t.Run(mode, func(t *testing.T) {
			b := New()
			r := vm.NewHostRecorder()
			b.RegisterIslandHostFactory("probe", func(string) vm.HostReceiver { return r })
			hydrateBatchReader(t, b, "reader", 1, true)
			r.Reset()
			events := 0
			b.SetSharedSignalCallback(func(string, string) { events++ })
			for i, value := range []int{1, 2, 2, 3, 3, 2} {
				if mode == "store" {
					b.GetStore().Set("$batch.0", vm.IntVal(value))
				} else if err := b.SetSharedSignalJSON("$batch.0", fmt.Sprint(value)); err != nil {
					t.Fatal(err)
				}
				if len(r.Calls) != 1 || r.Calls[0].Args[0].Int64() != int64(value) || r.Calls[0].Args[1].Int64() != int64(2*value) {
					t.Fatalf("write %d did not settle source=%d and derived=%d together: %+v", i, value, 2*value, r.Calls)
				}
				if events != i+1 {
					t.Fatalf("notifications = %d, want %d including repeated values", events, i+1)
				}
				r.Reset()
			}
		})
	}
}

func TestSharedSingleWriteIncludesNestedObserverUpdates(t *testing.T) {
	b := New()
	r := vm.NewHostRecorder()
	b.RegisterIslandHostFactory("probe", func(string) vm.HostReceiver { return r })
	hydrateBatchReader(t, b, "reader", 1, true)
	r.Reset()
	var events []string
	b.SetSharedSignalCallback(func(_, value string) {
		events = append(events, value)
		if value == "1" {
			b.GetStore().Set("$batch.0", vm.IntVal(2))
		}
	})
	b.GetStore().Set("$batch.0", vm.IntVal(1))
	if !reflect.DeepEqual(events, []string{"1", "2"}) || len(r.Calls) != 1 || r.Calls[0].Args[1].Int64() != 4 {
		t.Fatalf("nested update did not settle once: events=%q reads=%+v", events, r.Calls)
	}
}

func TestSharedSingleWriteMailboxUsesCurrentDerivedValue(t *testing.T) {
	b := New()
	p := batchReaderProgram(1, false)
	p.Exprs = append(p.Exprs,
		program.Expr{Op: program.OpAdd, Operands: []program.ExprID{0, 0}, Type: program.TypeInt},
		program.Expr{Op: program.OpSignalGet, Value: "twice", Type: program.TypeInt},
		program.Expr{Op: program.OpAdd, Operands: []program.ExprID{0, 3}, Type: program.TypeInt},
	)
	p.Computeds = []program.ComputedDef{{Name: "twice", Expr: 2, Type: program.TypeInt}}
	p.Nodes = []program.Node{{Kind: program.NodeExpr, Expr: 4}}
	if err := b.HydrateIsland("reader", p.Name, `{}`, encodeDispatchProgram(t, p), "json"); err != nil {
		t.Fatal(err)
	}
	var texts []string
	b.SetPatchMailboxCallback(func(_ string, data []byte) {
		_, _, patches, err := runtimewasm.DecodePatchMailbox(data)
		if err != nil || len(patches) != 1 || patches[0].Kind != vm.PatchSetText {
			t.Fatalf("invalid patches: %+v (%v)", patches, err)
		}
		texts = append(texts, patches[0].Text)
	})
	for value := 1; value <= 8; value++ {
		if err := b.SetSharedSignalJSON("$batch.0", fmt.Sprint(value)); err != nil {
			t.Fatal(err)
		}
		if len(texts) != value || texts[value-1] != fmt.Sprint(3*value) {
			t.Fatalf("write %d produced stale text: %q", value, texts)
		}
	}
}

func TestSharedSingleWriteAliasAndPanicRecovery(t *testing.T) {
	b := New()
	r := vm.NewHostRecorder()
	b.RegisterIslandHostFactory("probe", func(string) vm.HostReceiver { return r })
	p := batchReaderProgram(1, true)
	p.Signals[0].Name = "$scene.event.worldX"
	p.Exprs[0].Value = "$scene.event.worldX"
	if err := b.HydrateIsland("reader", p.Name, `{}`, encodeDispatchProgram(t, p), "json"); err != nil {
		t.Fatal(err)
	}
	r.Reset()
	b.GetStore().Set("$surface.event.worldX", vm.IntVal(1))
	r.Reset()
	b.SetSharedSignalCallback(func(string, string) { panic("observer") })
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		b.GetStore().Set("$surface.event.worldX", vm.IntVal(2))
	}()
	if recovered != "observer" {
		t.Fatalf("panic = %v, want observer", recovered)
	}
	b.SetSharedSignalCallback(nil)
	if err := b.SetSharedSignalJSON("$scene.event.worldX", "3"); err != nil {
		t.Fatal(err)
	}
	if len(r.Calls) != 1 || r.Calls[0].Args[0].Int64() != 3 || r.Calls[0].Args[1].Int64() != 6 {
		t.Fatalf("aliased reader did not recover without remounting: %+v", r.Calls)
	}
}

func TestSharedSingleWriteIndependentBridgesCanRunConcurrently(t *testing.T) {
	var done sync.WaitGroup
	for worker := range 4 {
		b := New()
		hydrateBatchReader(t, b, fmt.Sprint(worker), 1, false)
		done.Add(1)
		go func(b *Bridge) {
			defer done.Done()
			var lastText string
			b.SetPatchMailboxCallback(func(_ string, data []byte) {
				_, _, patches, err := runtimewasm.DecodePatchMailbox(data)
				if err != nil || len(patches) != 1 {
					t.Errorf("invalid mailbox: %+v (%v)", patches, err)
					return
				}
				lastText = patches[0].Text
			})
			for value := 1; value <= 100; value++ {
				if err := b.SetSharedSignalJSON("$batch.0", fmt.Sprint(value)); err != nil {
					t.Error(err)
					return
				}
			}
			if value, ok := b.GetStore().Get("$batch.0"); !ok || value.Int64() != 100 || lastText != "100" {
				t.Errorf("independent bridge lost its state: value=%v text=%q", value, lastText)
			}
		}(b)
	}
	done.Wait()
}

func BenchmarkSharedSingleWrite(b *testing.B) {
	for _, mode := range []string{"store", "json"} {
		for _, readers := range []int{1, 4} {
			b.Run(fmt.Sprintf("%s/readers=%d", mode, readers), func(b *testing.B) {
				bridge := New()
				bridge.SetSharedSignalCallback(func(string, string) {})
				for i := range readers {
					hydrateBatchReader(b, bridge, fmt.Sprint(i), 1, false)
				}
				payloads := []string{"1", "2"}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if mode == "store" {
						bridge.GetStore().Set("$batch.0", vm.IntVal((i&1)+1))
					} else if err := bridge.SetSharedSignalJSON("$batch.0", payloads[i&1]); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
