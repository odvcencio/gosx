package vm

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"m31labs.dev/gosx/island/program"
)

// ValueFromAny converts a decoded Go value into a VM value.
func ValueFromAny(value any) Value {
	return parseAnyValue(value)
}

// ValueFromJSON decodes arbitrary JSON into a VM value.
func ValueFromJSON(raw string) (Value, error) {
	var value any
	if err := decodeJSON(raw, &value); err != nil {
		return Value{}, err
	}
	return ValueFromAny(value), nil
}

// decodeJSON retains integer tokens and rejects trailing documents just like
// json.Unmarshal. Default interface decoding would round numbers before the VM
// could choose their representation.
func decodeJSON(raw string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("expected one JSON value")
	}
	switch value := target.(type) {
	case *any:
		return validateJSONNumbers(*value)
	case *map[string]any:
		return validateJSONNumbers(*value)
	}
	return nil
}

// UseNumber permits unbounded tokens. Retain Unmarshal's range errors for
// numbers that cannot be represented by either signed int64 or float64.
func validateJSONNumbers(value any) error {
	switch v := value.(type) {
	case json.Number:
		if _, err := v.Int64(); err == nil {
			return nil
		}
		_, err := v.Float64()
		return err
	case []any:
		for _, item := range v {
			if err := validateJSONNumbers(item); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range v {
			if err := validateJSONNumbers(item); err != nil {
				return err
			}
		}
	}
	return nil
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
		n := v.integer()
		if int64(int(n)) == n {
			return int(n)
		}
		return n
	case program.TypeFloat:
		return v.num
	default:
		return nil
	}
}
