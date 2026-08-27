package scene

// specularColorFromAny converts a legacy material map specular color entry in
// any of the accepted numeric array shapes (fixed array, float slice, or a
// generic list of numbers) into a fixed three-component linear RGB value.
// Invalid shapes report false so callers leave the field un-authored rather
// than silently writing an authored zero color.
func specularColorFromAny(v any) ([3]float64, bool) {
	switch c := v.(type) {
	case [3]float64:
		return c, true
	case []float64:
		if len(c) != 3 {
			return [3]float64{}, false
		}
		return [3]float64{c[0], c[1], c[2]}, true
	case []any:
		if len(c) != 3 {
			return [3]float64{}, false
		}
		var out [3]float64
		for i, e := range c {
			f, ok := toFloat64(e)
			if !ok {
				return [3]float64{}, false
			}
			out[i] = f
		}
		return out, true
	}
	return [3]float64{}, false
}

// copySpecularColor snapshots an authored specular color so later mutation of
// the source value cannot alias into lowered or canonical records.
func copySpecularColor(c *[3]float64) *[3]float64 {
	if c == nil {
		return nil
	}
	out := *c
	return &out
}

// setColor3Ptr writes an optional three-component linear RGB color into a
// legacy material map, omitting the key when absent and snapshotting the
// value so later mutation of the source cannot change the emitted map.
func setColor3Ptr(record map[string]any, key string, value *[3]float64) {
	if value == nil {
		return
	}
	c := *value
	record[key] = []float64{c[0], c[1], c[2]}
}
