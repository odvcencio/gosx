package aot

import (
	"fmt"
	"strings"

	"m31labs.dev/gosx/island/program"
)

type linkedShared struct {
	id, root uint32
	name     string
	kind     ScalarKind
}

type linkedProgram struct {
	unit   Unit
	state  *stateLayout
	dom    *domLayout
	inputs []uint32 // Frame-major input IDs; event leaves have no arena root.
}

// All programs address the same frame banks. Only the program assigned to an
// active manifest instance owns that frame; catalog programs need no instances.
// Capacity is fixed at the profile maximum, independently of SSR route guards.
type linkedLayout struct {
	programs                                                 []linkedProgram
	shared                                                   []linkedShared
	tags                                                     []string
	localStride, inputStride, computedStride, baselineStride uint32
	inputBase, sharedBase, computedBase, baselineBase, roots uint32
	frameTable, sharedVersions, tablesEnd                    uint32
	expressions                                              uint32
}

func buildLinkedLayout(units []Unit, options Options) (*linkedLayout, error) {
	ordered, _, _, err := canonicalUnits(units, options)
	if err != nil {
		return nil, err
	}
	l := &linkedLayout{}
	shared := map[string]ScalarKind{}
	tags := map[string]bool{}
	instances := make([]uint32, options.Limits.Instances)
	for i := range instances {
		instances[i] = uint32(i)
	}
	for _, unit := range ordered {
		state, dom, err := buildDOMLayout(unit, instances)
		if err != nil {
			return nil, err
		}
		locals, inputs := uint32(0), uint32(0)
		for _, signal := range unit.Contract.Signals {
			if !strings.HasPrefix(signal.Name, "$") {
				locals++
				continue
			}
			if kind, exists := shared[signal.Name]; exists && kind != signal.Kind {
				return nil, fmt.Errorf("incompatible shared declarations")
			}
			shared[signal.Name] = signal.Kind
		}
		for _, input := range unit.Contract.Inputs {
			if input.Source == "prop" {
				inputs++
			}
		}
		for _, tag := range dom.bindings.Tags {
			tags[tag] = true
		}
		l.localStride = max(l.localStride, locals)
		l.inputStride = max(l.inputStride, inputs)
		l.computedStride = max(l.computedStride, state.computedCount)
		l.baselineStride = max(l.baselineStride, uint32(len(dom.fields)))
		l.expressions = max(l.expressions, uint32(len(unit.Program.Exprs)))
		l.programs = append(l.programs, linkedProgram{unit: unit, state: state, dom: dom})
	}
	if len(shared) > int(options.Limits.SharedNames) {
		return nil, fmt.Errorf("linked shared names exceed the profile")
	}
	sharedNames := map[string]bool{}
	for name := range shared {
		sharedNames[name] = true
	}
	for i, name := range sortedBindingNames(sharedNames) {
		l.shared = append(l.shared, linkedShared{id: uint32(i), name: name, kind: shared[name]})
	}
	l.tags = sortedBindingNames(tags)
	capacity := options.Limits.Instances
	l.inputBase = capacity * l.localStride
	l.sharedBase = l.inputBase + capacity*l.inputStride
	l.computedBase = l.sharedBase + uint32(len(l.shared))
	l.baselineBase = l.computedBase + capacity*l.computedStride
	l.roots = l.baselineBase + capacity*l.baselineStride
	if l.roots > options.Limits.Values || uint64(l.roots+l.expressions)*valueBytes > 65536 {
		return nil, fmt.Errorf("linked scalar roots exceed the profile")
	}
	l.frameTable = computedMetaBase + capacity*l.computedStride*computedMetaBytes
	l.sharedVersions = l.frameTable + capacity*16
	l.tablesEnd = l.sharedVersions + uint32(len(l.shared))*16
	if l.tablesEnd > 32768 {
		return nil, fmt.Errorf("linked tables exceed the fixed interval")
	}
	sharedIDs := map[string]uint32{}
	for i := range l.shared {
		l.shared[i].root = l.sharedBase + uint32(i)
		sharedIDs[l.shared[i].name] = uint32(i)
	}
	tagIDs := bindingNameIDs(l.tags)
	for index := range l.programs {
		p := &l.programs[index]
		p.state.rows = nil
		p.state.mutableRoots = l.computedBase
		p.state.roots = l.roots
		p.dom.rootBase = l.baselineBase
		for i := range p.dom.bindings.Bindings {
			b := &p.dom.bindings.Bindings[i]
			if b.Kind == program.NodeElement {
				b.TagID = tagIDs[p.unit.Contract.Bindings[b.ID].Tag]
			}
		}
		p.dom.bindings.Tags = append([]string{}, l.tags...)
		for frame := uint32(0); frame < capacity; frame++ {
			local, input := uint32(0), uint32(0)
			for _, signal := range p.unit.Program.Signals {
				root := frame*l.localStride + local
				if shared, exists := sharedIDs[signal.Name]; exists {
					root = l.sharedBase + shared
				} else {
					local++
				}
				p.state.rows = append(p.state.rows, root)
			}
			for _, leaf := range p.unit.Contract.Inputs {
				root := NoBindingName
				if leaf.Source == "prop" {
					root = l.inputBase + frame*l.inputStride + input
					input++
				}
				p.inputs = append(p.inputs, root)
			}
		}
	}
	return l, nil
}
