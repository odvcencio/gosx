package bridge

import (
	"strings"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

// HostReceiverFactory creates one capability receiver per hydrated island.
// Factories are registered by the host runtime (the browser WASM entry point,
// a native shell, or a test harness), keeping host objects out of serialized
// island programs.
type HostReceiverFactory func(islandID string) vm.HostReceiver

// RegisterIslandHostFactory makes a named host capability available to future
// island hydrations. A nil factory unregisters the name. Existing islands keep
// their instance until reload or disposal, so registration cannot invalidate a
// handler that is currently dispatching.
func (b *Bridge) RegisterIslandHostFactory(name string, factory HostReceiverFactory) {
	if b == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if b.hostFactories == nil {
		b.hostFactories = make(map[string]HostReceiverFactory)
	}
	if factory == nil {
		delete(b.hostFactories, name)
		return
	}
	b.hostFactories[name] = factory
}

func (b *Bridge) bindIslandHosts(islandID string, island *vm.Island, prog *program.Program) {
	if b == nil || island == nil || len(b.hostFactories) == 0 {
		return
	}
	referenced := referencedHostReceivers(prog)
	if len(referenced) == 0 {
		return
	}
	bindings := make(map[string]vm.HostReceiver, len(referenced))
	for name := range referenced {
		factory := b.hostFactories[name]
		if factory == nil {
			continue
		}
		receiver := factory(islandID)
		if receiver == nil {
			continue
		}
		island.BindHost(name, receiver)
		bindings[name] = receiver
	}
	if len(bindings) > 0 {
		if b.hostReceivers == nil {
			b.hostReceivers = make(map[string]map[string]vm.HostReceiver)
		}
		b.hostReceivers[islandID] = bindings
	}
}

func (b *Bridge) disposeIslandHosts(islandID string, island *vm.Island) {
	if b == nil {
		return
	}
	bindings := b.hostReceivers[islandID]
	delete(b.hostReceivers, islandID)
	for name, receiver := range bindings {
		island.BindHost(name, nil)
		if disposable, ok := receiver.(interface{ Dispose() }); ok {
			disposable.Dispose()
		}
	}
}

func referencedHostReceivers(prog *program.Program) map[string]struct{} {
	if prog == nil {
		return nil
	}
	var names map[string]struct{}
	for i := range prog.Exprs {
		expr := &prog.Exprs[i]
		if expr.Op != program.OpHostCall {
			continue
		}
		dot := strings.IndexByte(expr.Value, '.')
		if dot <= 0 {
			continue
		}
		if names == nil {
			names = make(map[string]struct{})
		}
		names[expr.Value[:dot]] = struct{}{}
	}
	return names
}
