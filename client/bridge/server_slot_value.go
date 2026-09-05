package bridge

import (
	"fmt"
	"m31labs.dev/gosx/client/vm"
)

func copyServerSlotValue(value vm.Value, depth int) (vm.Value, error) {
	if depth > 64 || vm.IsClosure(value) {
		return vm.Value{}, fmt.Errorf("ServerSlot: shared snapshot requires finite data, not closures")
	}
	if value.IsList() {
		out := make([]vm.Value, len(value.List()))
		for i, child := range value.List() {
			copy, err := copyServerSlotValue(child, depth+1)
			if err != nil {
				return vm.Value{}, err
			}
			out[i] = copy
		}
		return vm.ArrayVal(out), nil
	}
	if value.IsMap() {
		out := make(map[string]vm.Value, len(value.Map()))
		for key, child := range value.Map() {
			copy, err := copyServerSlotValue(child, depth+1)
			if err != nil {
				return vm.Value{}, err
			}
			out[key] = copy
		}
		return vm.ObjectVal(out), nil
	}
	return value, nil
}
