package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// validateSpecular diagnoses authored specular factors. Intensity and color
// are validated independently so one invalid factor never hides the other.
// Absent factors are valid and mean the renderer default; explicit zero is
// valid; intensity must be finite and within [0, 1]; color channels must be
// finite and nonnegative with no upper clamp (HDR linear factors allowed).
func validateSpecular(report *Report, id, path string, intensity *float64, color *[3]float64) {
	if intensity != nil {
		ipath := path + ".specularIntensity"
		value := *intensity
		switch {
		case !finite(value):
			report.add(Error, "scene.material.specular_intensity_non_finite", "Specular intensity must be finite", ipath, id, nil)
		case value < 0:
			report.add(Error, "scene.material.specular_intensity_negative", "Specular intensity must not be negative", ipath, id, map[string]any{"intensity": value})
		case value > 1:
			report.add(Error, "scene.material.specular_intensity_out_of_range", "Specular intensity must be between 0 and 1", ipath, id, map[string]any{"intensity": value})
		}
	}
	if color != nil {
		for i, channel := range color {
			cpath := fmt.Sprintf("%s.specularColor[%d]", path, i)
			switch {
			case !finite(channel):
				report.add(Error, "scene.material.specular_color_non_finite", "Specular color channel must be finite", cpath, id, nil)
			case channel < 0:
				report.add(Error, "scene.material.specular_color_negative", "Specular color channel must not be negative", cpath, id, map[string]any{"channel": channel})
			}
		}
	}
}

// rawSpecularRecord mirrors only the fields the raw shape pass needs. The
// typed decoder silently pads short color arrays and zeroes null elements,
// so those shapes must be diagnosed from the original JSON.
type rawSpecularRecord struct {
	ID            string          `json:"id"`
	SpecularColor json.RawMessage `json:"specularColor"`
}

type rawSpecularDocument struct {
	Objects            []rawSpecularRecord `json:"objects"`
	Models             []rawSpecularRecord `json:"models"`
	InstancedMeshes    []rawSpecularRecord `json:"instancedMeshes"`
	InstancedGLBMeshes []rawSpecularRecord `json:"instancedGLBMeshes"`
}

// validateSpecularRawDocument scans the raw JSON for specular color arrays
// whose shapes encoding/json silently accepts during typed decoding: arrays
// shorter than three elements (padded with zeros) and null elements
// (silently zeroed). Over-long arrays are also covered here: decoding into
// a fixed-size typed array would silently discard the extra elements, so
// the raw scan inspects the full element list. Wrong-typed channels are
// reported by the typed decoder; this pass adds no diagnostic for them.
// A json.Decoder is used so exactly the first JSON value is scanned,
// matching ValidateJSON's own decoding behavior.
func validateSpecularRawDocument(report *Report, data []byte) {
	var raw rawSpecularDocument
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&raw); err != nil {
		return // the typed pass already reports this decode failure
	}
	families := []struct {
		name    string
		records []rawSpecularRecord
	}{
		{"objects", raw.Objects},
		{"models", raw.Models},
		{"instancedMeshes", raw.InstancedMeshes},
		{"instancedGLBMeshes", raw.InstancedGLBMeshes},
	}
	for _, family := range families {
		for i, record := range family.records {
			validateSpecularColorShape(report, record.ID, fmt.Sprintf("%s[%d].specularColor", family.name, i), record.SpecularColor)
		}
	}
}

// validateSpecularColorShape reports color arrays that are not exactly three
// non-null elements. Numeric, finite, and nonnegative constraints are
// enforced by typed decoding plus validateSpecular, not here. A whole-value
// null is absent (optional), not a shape error.
func validateSpecularColorShape(report *Report, id, path string, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	if string(bytes.TrimSpace(raw)) == "null" {
		return // a whole-value null is absent (optional)
	}
	var elements []json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&elements); err != nil {
		return // the typed decoder already rejected this input
	}
	if len(elements) != 3 {
		report.add(Error, "scene.material.specular_color_invalid_shape", "Specular color must be an array of exactly three numbers", path, id, map[string]any{"elements": len(elements)})
		return
	}
	for i, element := range elements {
		if string(bytes.TrimSpace(element)) == "null" {
			report.add(Error, "scene.material.specular_color_invalid_shape", "Specular color must not contain null elements", fmt.Sprintf("%s[%d]", path, i), id, nil)
		}
	}
}
