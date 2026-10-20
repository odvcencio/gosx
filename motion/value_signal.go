package motion

import (
	"fmt"
	"strings"
)

// Resolve shapes once at authoring time; the browser graph can then share one
// vector sample without silently coercing it through scalar operators.
func programValueArities(signals []SignalSpec) (map[SignalRef]ValueArity, error) {
	specs := make(map[SignalRef]SignalSpec, len(signals))
	for _, signal := range signals {
		specs[SignalRef(strings.TrimSpace(signal.ID))] = signal
	}
	arities := make(map[SignalRef]ValueArity, len(signals))
	visiting := make(map[SignalRef]bool)
	var resolve func(SignalRef, int) (ValueArity, error)
	resolve = func(ref SignalRef, depth int) (ValueArity, error) {
		if ref == "" {
			return ArityScalar, nil
		}
		if arity, ok := arities[ref]; ok {
			return arity, nil
		}
		if visiting[ref] {
			return 0, fmt.Errorf("motion: cyclic signal dependency at %q", ref)
		}
		if depth > 1024 {
			return 0, fmt.Errorf("motion: signal dependency nesting exceeds 1024 at %q", ref)
		}
		visiting[ref] = true
		signal := specs[ref]
		arity := ArityScalar
		if signal.Kind == SignalTrack {
			arity = signal.Arity
		}
		for _, input := range []SignalRef{signal.Input, signal.A, signal.B, signal.Weight} {
			inputArity, err := resolve(input, depth+1)
			if err != nil {
				return 0, err
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
		delete(visiting, ref)
		arities[ref] = arity
		return arity, nil
	}
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
