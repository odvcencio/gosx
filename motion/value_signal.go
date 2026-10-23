package motion

import (
	"fmt"
	"strings"
)

// Resolve shapes once at authoring time; the browser graph can then share one
// vector sample without silently coercing it through scalar operators.
func programValueArities(signals []SignalSpec) (map[SignalRef]ValueArity, error) {
	specs := make(map[SignalRef]int, len(signals))
	for i, signal := range signals {
		specs[SignalRef(strings.TrimSpace(signal.ID))] = i
	}
	arities := make(map[SignalRef]ValueArity, len(signals))
	visiting := make([]bool, len(signals))
	pathDepth := make([]int, len(signals))
	var resolve func(SignalRef, int) (ValueArity, error)
	resolve = func(ref SignalRef, depth int) (ValueArity, error) {
		if ref == "" {
			return ArityScalar, nil
		}
		index, exists := specs[ref]
		if !exists {
			return 0, fmt.Errorf("motion: unknown signal %q", ref)
		}
		if depth+pathDepth[index] > 1024 {
			return 0, fmt.Errorf("motion: signal dependency nesting exceeds 1024 at %q", ref)
		}
		if arity, ok := arities[ref]; ok {
			return arity, nil
		}
		if visiting[index] {
			return 0, fmt.Errorf("motion: cyclic signal dependency at %q", ref)
		}
		visiting[index] = true
		signal := &signals[index]
		arity := ArityScalar
		if signal.Kind == SignalTrack {
			arity = signal.Arity
		}
		for _, input := range []SignalRef{signal.Input, signal.A, signal.B, signal.Weight} {
			if input == "" {
				continue
			}
			inputArity, err := resolve(input, depth+1)
			if err != nil {
				return 0, err
			}
			pathDepth[index] = max(pathDepth[index], pathDepth[specs[input]]+1)
			if pathDepth[index] > 1024 {
				return 0, fmt.Errorf("motion: signal dependency nesting exceeds 1024 at %q", ref)
			}
			if inputArity == ArityScalar {
				continue
			}
			if signal.Kind != SignalMix || input == signal.Weight {
				return 0, fmt.Errorf("motion: signal %q requires scalar input %q", ref, input)
			}
		}
		if signal.Kind == SignalMix {
			a, err := resolve(signal.A, depth+1)
			if err != nil {
				return 0, err
			}
			b, err := resolve(signal.B, depth+1)
			if err != nil {
				return 0, err
			}
			if a != b || a == ArityQuat {
				return 0, fmt.Errorf("motion: mix %q needs matching non-quaternion values", ref)
			}
			arity = a
		}
		visiting[index] = false
		arities[ref] = arity
		return arity, nil
	}
	//gosx:allow map-range validation only; any unresolved reference rejects the specs
	for ref := range specs {
		if _, err := resolve(ref, 0); err != nil {
			return nil, err
		}
	}
	return arities, nil
}

func validateBindingArity(binding Binding, arity ValueArity) error {
	if binding.Target == BindingMaterialUniform || binding.Target == BindingCSSVariable {
		return nil
	}
	want := ArityScalar
	switch binding.Target {
	case BindingSceneNode, BindingCamera:
		switch binding.Property {
		case "position", "scale":
			want = ArityVec3
		case "rotation":
			want = ArityQuat
		}
	case BindingStyle:
		switch binding.Property {
		case "color", "background", "backgroundColor", "background-color":
			if arity == ArityColor {
				return nil
			}
		}
	}
	if arity != want {
		return fmt.Errorf("motion: %s binding %q needs %s values, got %s signal %q", binding.Target, binding.Property, valueArityName(want), valueArityName(arity), binding.Signal)
	}
	return nil
}

func valueArityName(arity ValueArity) string {
	names := [...]string{"scalar", "vec2", "vec3", "vec4", "quaternion", "color"}
	if arity > ArityColor {
		return "invalid"
	}
	return names[arity]
}
