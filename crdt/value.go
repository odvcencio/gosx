package crdt

import (
	"encoding/base64"
	"fmt"
	"time"
)

type ObjID string
type Prop string

const Root ObjID = "root"

type ValueKind string

const (
	ValueKindNull      ValueKind = "null"
	ValueKindBool      ValueKind = "bool"
	ValueKindInt       ValueKind = "int"
	ValueKindUint      ValueKind = "uint"
	ValueKindFloat     ValueKind = "float"
	ValueKindString    ValueKind = "string"
	ValueKindBytes     ValueKind = "bytes"
	ValueKindCounter   ValueKind = "counter"
	ValueKindTimestamp ValueKind = "timestamp"
	ValueKindMap       ValueKind = "map"
	ValueKindList      ValueKind = "list"
	ValueKindText      ValueKind = "text"
)

type Value struct {
	Kind    ValueKind  `json:"kind"`
	Bool    bool       `json:"bool,omitempty"`
	Int     int64      `json:"int,omitempty"`
	Uint    uint64     `json:"uint,omitempty"`
	Float   float64    `json:"float,omitempty"`
	Str     string     `json:"str,omitempty"`
	Bytes   []byte     `json:"bytes,omitempty"`
	Counter int64      `json:"counter,omitempty"`
	Time    *time.Time `json:"time,omitempty"`
	Obj     ObjID      `json:"obj,omitempty"`
}

func NullValue() Value                 { return Value{Kind: ValueKindNull} }
func BoolValue(v bool) Value           { return Value{Kind: ValueKindBool, Bool: v} }
func IntValue(v int64) Value           { return Value{Kind: ValueKindInt, Int: v} }
func UintValue(v uint64) Value         { return Value{Kind: ValueKindUint, Uint: v} }
func FloatValue(v float64) Value       { return Value{Kind: ValueKindFloat, Float: v} }
func StringValue(v string) Value       { return Value{Kind: ValueKindString, Str: v} }
func BytesValue(v []byte) Value        { return Value{Kind: ValueKindBytes, Bytes: append([]byte(nil), v...)} }
func CounterValue(v int64) Value       { return Value{Kind: ValueKindCounter, Counter: v} }
func TimestampValue(v time.Time) Value { return Value{Kind: ValueKindTimestamp, Time: &v} }
func MapValue(obj ObjID) Value         { return Value{Kind: ValueKindMap, Obj: obj} }
func ListValue(obj ObjID) Value        { return Value{Kind: ValueKindList, Obj: obj} }
func TextValue(obj ObjID) Value        { return Value{Kind: ValueKindText, Obj: obj} }

func (v Value) IsObject() bool {
	return v.Kind == ValueKindMap || v.Kind == ValueKindList || v.Kind == ValueKindText
}

func (v Value) Clone() Value {
	out := v
	if v.Bytes != nil {
		out.Bytes = append([]byte(nil), v.Bytes...)
	}
	if v.Time != nil {
		value := *v.Time
		out.Time = &value
	}
	return out
}

func (v Value) ToAny() any {
	switch v.Kind {
	case ValueKindNull:
		return nil
	case ValueKindBool:
		return v.Bool
	case ValueKindInt:
		return v.Int
	case ValueKindUint:
		return v.Uint
	case ValueKindFloat:
		return v.Float
	case ValueKindString:
		return v.Str
	case ValueKindBytes:
		return append([]byte(nil), v.Bytes...)
	case ValueKindCounter:
		return v.Counter
	case ValueKindTimestamp:
		if v.Time == nil {
			return nil
		}
		return v.Time.Format(time.RFC3339Nano)
	case ValueKindMap, ValueKindList, ValueKindText:
		return string(v.Obj)
	default:
		return nil
	}
}

func ValueFromAny(raw any) (Value, error) {
	switch value := raw.(type) {
	case nil:
		return NullValue(), nil
	case bool:
		return BoolValue(value), nil
	case int:
		return IntValue(int64(value)), nil
	case int8:
		return IntValue(int64(value)), nil
	case int16:
		return IntValue(int64(value)), nil
	case int32:
		return IntValue(int64(value)), nil
	case int64:
		return IntValue(value), nil
	case uint:
		return UintValue(uint64(value)), nil
	case uint8:
		return UintValue(uint64(value)), nil
	case uint16:
		return UintValue(uint64(value)), nil
	case uint32:
		return UintValue(uint64(value)), nil
	case uint64:
		return UintValue(value), nil
	case float32:
		return FloatValue(float64(value)), nil
	case float64:
		return FloatValue(value), nil
	case string:
		return StringValue(value), nil
	case []byte:
		return BytesValue(value), nil
	case time.Time:
		return TimestampValue(value.UTC()), nil
	default:
		return Value{}, fmt.Errorf("unsupported crdt value type %T", raw)
	}
}

type jsonValue struct {
	Kind    ValueKind `json:"kind"`
	Bool    bool      `json:"bool,omitempty"`
	Int     int64     `json:"int,omitempty"`
	Uint    uint64    `json:"uint,omitempty"`
	Float   float64   `json:"float,omitempty"`
	Str     string    `json:"str,omitempty"`
	Bytes   string    `json:"bytes,omitempty"`
	Counter int64     `json:"counter,omitempty"`
	Time    string    `json:"time,omitempty"`
	Obj     ObjID     `json:"obj,omitempty"`
}

func (v Value) MarshalJSON() ([]byte, error) {
	out := jsonValue{
		Kind:    v.Kind,
		Bool:    v.Bool,
		Int:     v.Int,
		Uint:    v.Uint,
		Float:   v.Float,
		Str:     v.Str,
		Counter: v.Counter,
		Obj:     v.Obj,
	}
	if len(v.Bytes) > 0 {
		out.Bytes = base64.StdEncoding.EncodeToString(v.Bytes)
	}
	if v.Time != nil {
		out.Time = v.Time.UTC().Format(time.RFC3339Nano)
	}
	return marshalJSON(out)
}

func (v *Value) UnmarshalJSON(data []byte) error {
	var decoded jsonValue
	if err := unmarshalJSON(data, &decoded); err != nil {
		return err
	}
	out := Value{
		Kind:    decoded.Kind,
		Bool:    decoded.Bool,
		Int:     decoded.Int,
		Uint:    decoded.Uint,
		Float:   decoded.Float,
		Str:     decoded.Str,
		Counter: decoded.Counter,
		Obj:     decoded.Obj,
	}
	if decoded.Bytes != "" {
		bytes, err := base64.StdEncoding.DecodeString(decoded.Bytes)
		if err != nil {
			return fmt.Errorf("decode bytes value: %w", err)
		}
		out.Bytes = bytes
	}
	if decoded.Time != "" {
		value, err := time.Parse(time.RFC3339Nano, decoded.Time)
		if err != nil {
			return fmt.Errorf("decode timestamp value: %w", err)
		}
		out.Time = &value
	}
	*v = out
	return nil
}
