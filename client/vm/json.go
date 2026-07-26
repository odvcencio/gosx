package vm

import (
	"encoding/json"

	"m31labs.dev/gosx/island/program"
)

// ValueFromAny converts a decoded Go value into a VM value.
func ValueFromAny(value any) Value {
	return parseAnyValue(value)
}

// ValueFromJSON decodes arbitrary JSON into a VM value.
func ValueFromJSON(raw string) (Value, error) {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return Value{}, err
	}
	return ValueFromAny(value), nil
}

// ToAny converts a VM value back into plain Go values suitable for JSON marshaling.
func (v Value) ToAny() any {
	return v.toAnyAtDepth(0)
}

// toAnyAtDepth mirrors the depth guard in Value.String and
// Value.Eq. A Value can contain itself, built through OpIndexSet or
// OpFieldSet's in-place mutation. Such a Value must degrade to a
// sentinel instead of recursing until the goroutine stack
// overflows.
func (v Value) toAnyAtDepth(depth int) any {
	if depth > maxValueRecursionDepth {
		return cycleSentinel
	}
	if v.isList() {
		src := v.list()
		items := make([]any, len(src))
		for i := range src {
			items[i] = src[i].toAnyAtDepth(depth + 1)
		}
		return items
	}
	if v.isMap() {
		src := v.dict()
		fields := make(map[string]any, len(src))
		for key, field := range src {
			fields[key] = field.toAnyAtDepth(depth + 1)
		}
		return fields
	}

	switch v.Type {
	case program.TypeString:
		return v.text()
	case program.TypeBool:
		return v.truth()
	case program.TypeInt:
		return int(v.num)
	case program.TypeFloat:
		return v.num
	default:
		return nil
	}
}
