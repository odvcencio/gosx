package budget

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"unicode/utf8"
)

func decodeInput(data []byte, definition string, out any) error {
	if len(data) > maxInputBytes || !utf8.Valid(data) {
		return errors.New("invalid input encoding or size")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := scanJSON(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("input must contain one JSON value")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return errors.New("invalid input JSON")
	}
	if err := validateInput(value, inputDefinitions[definition]); err != nil {
		return err
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return errors.New("input does not match typed contract")
	}
	return nil
}

func scanJSON(d *json.Decoder, depth int) error {
	if depth > 64 {
		return errors.New("input nesting exceeds limit")
	}
	token, err := d.Token()
	if err != nil {
		return errors.New("invalid input JSON")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	seen := make(map[string]bool)
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return errors.New("invalid input JSON")
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate or invalid JSON member")
			}
			seen[name] = true
		}
		if err := scanJSON(d, depth+1); err != nil {
			return err
		}
	}
	end, err := d.Token()
	if err != nil || delim == '{' && end != json.Delim('}') || delim == '[' && end != json.Delim(']') {
		return errors.New("invalid input JSON")
	}
	return nil
}

func validateNumber(value json.Number, s map[string]any) error {
	fail := errors.New("input number does not match schema")
	if s["type"] == "integer" {
		if _, err := value.Int64(); err != nil {
			return fail
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
