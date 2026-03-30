package vm

import (
	"encoding/json"

	"github.com/odvcencio/gosx/island/program"
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
	if v.Items != nil {
		items := make([]any, len(v.Items))
		for i := range v.Items {
			items[i] = v.Items[i].ToAny()
		}
		return items
	}
	if v.Fields != nil {
		fields := make(map[string]any, len(v.Fields))
		for key, field := range v.Fields {
			fields[key] = field.ToAny()
		}
		return fields
	}

	switch v.Type {
	case program.TypeString:
		return v.Str
	case program.TypeBool:
		return v.Bool
	case program.TypeInt:
		return int(v.Num)
	case program.TypeFloat:
		return v.Num
	default:
		return nil
	}
}
