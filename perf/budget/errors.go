package budget

import (
	"errors"
	"strings"
)

// InputError identifies a rejected input without retaining its values or path.
// Pointer uses JSON Pointer escaping. Reference is a code-controlled label.
type InputError struct {
	Code      string `json:"code"`
	Reference string `json:"reference"`
	Pointer   string `json:"pointer"`
}

func (e *InputError) Error() string { return e.Code + ": " + e.Reference + "#" + e.Pointer }
func invalidInput(pointer string) *InputError {
	return &InputError{Code: "invalid-input", Pointer: pointer}
}
func pointerChild(parent, key string) string {
	return parent + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
func referenceLabel(definition string) string {
	switch definition {
	case "Profile":
		return "profile"
	case "Coefficients":
		return "coefficients"
	case "Toolchain":
		return "toolchain"
	case "FixtureCatalog", "FixtureManifest", "AssetUse":
		return "fixtures"
	case "Budget":
		return "budget"
	case "Report":
		return "report"
	case "SeriesPoint":
		return "series"
	case "PairReport":
		return "pair"
	case "FieldSnapshot":
		return "field"
	case "RunStatus":
		return "run-status"
	default:
		return "input"
	}
}
func inputReference(err error, reference, pointer string) error {
	if err == nil {
		return nil
	}
	out := invalidInput(pointer)
	var input *InputError
	if errors.As(err, &input) {
		*out = *input
	}
	if out.Reference == "" {
		out.Reference = reference
	}
	return out
}
