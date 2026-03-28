//go:build js && wasm

package textlayout

import (
	"encoding/json"
	"fmt"
	"syscall/js"
)

// BrowserMeasurer delegates width measurement to the JS bootstrap helper.
type BrowserMeasurer struct {
	fn js.Value
}

// NewBrowserMeasurer binds the bootstrap helper exposed on window.
func NewBrowserMeasurer() (*BrowserMeasurer, error) {
	fn := js.Global().Get("__gosx_measure_text_batch")
	if !fn.Truthy() {
		return nil, fmt.Errorf("textlayout: __gosx_measure_text_batch is not available")
	}
	return &BrowserMeasurer{fn: fn}, nil
}

// MeasureBatch measures token widths using browser font metrics.
func (m *BrowserMeasurer) MeasureBatch(font string, texts []string) ([]float64, error) {
	if m == nil || !m.fn.Truthy() {
		return nil, fmt.Errorf("textlayout: browser measurer is not initialized")
	}
	if len(texts) == 0 {
		return []float64{}, nil
	}

	payload, err := json.Marshal(texts)
	if err != nil {
		return nil, err
	}

	raw := m.fn.Invoke(font, string(payload))
	if raw.Type() == js.TypeString {
		var widths []float64
		if err := json.Unmarshal([]byte(raw.String()), &widths); err != nil {
			return nil, fmt.Errorf("textlayout: decode browser widths: %w", err)
		}
		if len(widths) != len(texts) {
			return nil, fmt.Errorf("textlayout: browser measurer returned %d widths for %d texts", len(widths), len(texts))
		}
		return widths, nil
	}

	arrayCtor := js.Global().Get("Array")
	if arrayCtor.Truthy() && raw.InstanceOf(arrayCtor) {
		length := raw.Length()
		widths := make([]float64, length)
		for i := range length {
			widths[i] = raw.Index(i).Float()
		}
		if len(widths) != len(texts) {
			return nil, fmt.Errorf("textlayout: browser measurer returned %d widths for %d texts", len(widths), len(texts))
		}
		return widths, nil
	}

	return nil, fmt.Errorf("textlayout: unsupported browser measurer return type %v", raw.Type())
}
