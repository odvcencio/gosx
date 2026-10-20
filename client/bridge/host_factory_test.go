package bridge

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

type disposableHostRecorder struct {
	*vm.HostRecorder
	disposed *int
}

func (r *disposableHostRecorder) Dispose() {
	*r.disposed++
}

func hostCallProgram(receiver, method string) *program.Program {
	return &program.Program{
		Name:     "HostCaller",
		Nodes:    []program.Node{{Kind: program.NodeElement, Tag: "div"}},
		Exprs:    []program.Expr{{Op: program.OpHostCall, Value: receiver + "." + method, Type: program.TypeAny}},
		Handlers: []program.Handler{{Name: "call", Body: []program.ExprID{0}}},
	}
}

func TestBridgeBindsOnlyReferencedHostFactoriesAndDisposesReceivers(t *testing.T) {
	b := New()
	created := 0
	unreferencedCreated := 0
	disposed := 0
	var receiver *disposableHostRecorder
	b.RegisterIslandHostFactory("browser", func(islandID string) vm.HostReceiver {
		created++
		if islandID != "island-0" {
			t.Fatalf("factory island id = %q", islandID)
		}
		receiver = &disposableHostRecorder{HostRecorder: vm.NewHostRecorder(), disposed: &disposed}
		return receiver
	})
	b.RegisterIslandHostFactory("unused", func(string) vm.HostReceiver {
		unreferencedCreated++
		return vm.NewHostRecorder()
	})

	data, err := program.EncodeJSON(hostCallProgram("browser", "Focus"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.HydrateIsland("island-0", "HostCaller", `{}`, data, "json"); err != nil {
		t.Fatal(err)
	}
	if created != 1 || unreferencedCreated != 0 {
		t.Fatalf("factory calls browser/unused = %d/%d, want 1/0", created, unreferencedCreated)
	}
	if _, err := b.DispatchAction("island-0", "call", `{}`); err != nil {
		t.Fatal(err)
	}
	if len(receiver.Calls) != 1 || receiver.Calls[0].Method != "Focus" {
		t.Fatalf("host calls = %+v", receiver.Calls)
	}

	b.DisposeIsland("island-0")
	if disposed != 1 {
		t.Fatalf("receiver Dispose calls = %d, want 1", disposed)
	}
	if len(b.hostReceivers) != 0 {
		t.Fatalf("live host receiver sets after dispose = %d", len(b.hostReceivers))
	}
}

func TestBridgeReloadRebindsReferencedHostReceiver(t *testing.T) {
	b := New()
	created := 0
	disposed := 0
	b.RegisterIslandHostFactory("browser", func(string) vm.HostReceiver {
		created++
		return &disposableHostRecorder{HostRecorder: vm.NewHostRecorder(), disposed: &disposed}
	})

	data, _ := program.EncodeJSON(hostCallProgram("browser", "Focus"))
	if err := b.HydrateIsland("island-0", "HostCaller", `{}`, data, "json"); err != nil {
		t.Fatal(err)
	}
	if err := b.ReloadProgram("island-0", data, "json"); err != nil {
		t.Fatal(err)
	}
	if created != 2 || disposed != 1 {
		t.Fatalf("after reload created/disposed = %d/%d, want 2/1", created, disposed)
	}
	b.DisposeIsland("island-0")
	if disposed != 2 {
		t.Fatalf("after final dispose count = %d, want 2", disposed)
	}
}
