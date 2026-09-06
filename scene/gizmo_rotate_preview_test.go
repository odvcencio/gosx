package scene

import (
	"encoding/json"
	"testing"
)

func TestGizmoLocalRotatePreviewOptInProps(t *testing.T) {
	for _, flag := range []*bool{nil, Bool(false), Bool(true)} {
		props := Props{GizmoLocalRotatePreview: flag, SelectionInputSignal: "selected"}
		for _, data := range []map[string]any{props.LegacyProps(), props.GoSXSpreadProps()} {
			value, present := data["gizmoLocalRotatePreview"]
			if present != (flag != nil) || (present && value != *flag) {
				t.Fatalf("flag%v value%v present%v", flag, value, present)
			}
		}
		var data map[string]any
		if err := json.Unmarshal(props.RawPropsJSON(), &data); err != nil {
			t.Fatal(err)
		}
		value, present := data["gizmoLocalRotatePreview"]
		if present != (flag != nil) || (present && value != *flag) {
			t.Fatalf("JSON flag%v value%v", flag, value)
		}
	}
}
