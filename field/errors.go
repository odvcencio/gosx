package field

import "fmt"

func fieldError(op, format string, args ...any) error {
	return fmt.Errorf(op+": "+format, args...)
}

func validateNewArgs(op string, resolution [3]int, components int) error {
	if components < 1 || components > 4 {
		return fieldError(op, "components must be 1..4, got %d", components)
	}
	if resolution[0] < 1 || resolution[1] < 1 || resolution[2] < 1 {
		return fieldError(op, "resolution must be >= 1 on every axis, got %v", resolution)
	}
	return nil
}

func validateField(op string, f *Field) error {
	if f == nil {
		return fieldError(op, "field is nil")
	}
	if err := validateNewArgs(op, f.Resolution, f.Components); err != nil {
		return err
	}
	want := f.Resolution[0] * f.Resolution[1] * f.Resolution[2] * f.Components
	if len(f.Data) < want {
		return fieldError(op, "data length %d is smaller than required shape length %d", len(f.Data), want)
	}
	return nil
}

func validateFieldComponents(op string, f *Field, components int) error {
	if err := validateField(op, f); err != nil {
		return err
	}
	if f.Components != components {
		return fieldError(op, "field must have Components == %d, got %d", components, f.Components)
	}
	return nil
}

func validateSameShape(op string, a, b *Field) error {
	if err := validateField(op, a); err != nil {
		return err
	}
	if err := validateField(op, b); err != nil {
		return err
	}
	if a.Resolution != b.Resolution || a.Components != b.Components {
		return fieldError(op, "shape mismatch: %v/%d != %v/%d", a.Resolution, a.Components, b.Resolution, b.Components)
	}
	return nil
}
