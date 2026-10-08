package budget

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

func decodeInput(data []byte, definition string, out any) (resultErr error) {
	defer func() { resultErr = inputReference(resultErr, referenceLabel(definition), "") }()
	if len(data) > maxInputBytes || !utf8.Valid(data) {
		return invalidInput("")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := scanJSON(d, 0, inputDefinitions[definition], ""); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return invalidInput("")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return invalidInput("")
	}
	if err := validateInput(value, inputDefinitions[definition]); err != nil {
		return err
	}
	// Typed decoding consumes the validated value, never a second object merge
	// from the original input. The token scan also rejects duplicate keys.
	validated, err := json.Marshal(value)
	if err != nil {
		return invalidInput("")
	}
	d = json.NewDecoder(bytes.NewReader(validated))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return invalidInput("")
	}
	return nil
}

func scanSchema(raw any) map[string]any {
	s, _ := raw.(map[string]any)
	for s != nil {
		ref, ok := s["$ref"].(string)
		if !ok {
			break
		}
		s, _ = inputDefinitions[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
	}
	return s
}

func scanJSON(d *json.Decoder, depth int, raw any, pointer string) error {
	if depth > 64 {
		return invalidInput(pointer)
	}
	token, err := d.Token()
	if err != nil {
		return invalidInput(pointer)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	s := scanSchema(raw)
	seen := make(map[string]bool)
	index := 0
	for d.More() {
		childPointer := pointer
		var childSchema any
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return invalidInput(pointer)
			}
			name, ok := key.(string)
			props, _ := s["properties"].(map[string]any)
			if prop, known := props[name]; known {
				childSchema = prop
				childPointer = pointerChild(pointer, name)
			} else {
				childSchema = s["additionalProperties"]
			}
			if !ok || seen[name] {
				return invalidInput(childPointer)
			}
			seen[name] = true
		} else {
			childPointer = pointerChild(pointer, strconv.Itoa(index))
			childSchema = s["items"]
		}
		if err := scanJSON(d, depth+1, childSchema, childPointer); err != nil {
			return err
		}
		index++
	}
	end, err := d.Token()
	if err != nil || delim == '{' && end != json.Delim('}') || delim == '[' && end != json.Delim(']') {
		return invalidInput(pointer)
	}
	return nil
}

func validateNumber(value json.Number, s map[string]any) error {
	fail := invalidInput("")
	if s["type"] == "integer" {
		if _, err := value.Int64(); err != nil {
			if _, err := strconv.ParseUint(string(value), 10, 64); err != nil {
				return fail
			}
		}
	}
	n, err := value.Float64()
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return fail
	}
	if low, ok := s["minimum"].(float64); ok && n < low {
		return fail
	}
	if high, ok := s["maximum"].(float64); ok && n > high {
		return fail
	}
	return nil
}
