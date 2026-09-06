package scene

import (
	"encoding/json"
	"testing"
)

func TestGizmoCommitActionProps(t *testing.T) {
	for _, action := range []string{"", "/edit/__actions/transform"} {
		props := Props{GizmoCommitAction: action, SelectionInputSignal: "selected"}
		var raw map[string]any
		if err := json.Unmarshal(props.RawPropsJSON(), &raw); err != nil {
			t.Fatal(err)
		}
		for _, data := range []map[string]any{raw, props.LegacyProps(), props.GoSXSpreadProps()} {
			got, ok := data["gizmoCommitAction"]
			if ok != (action != "") || (ok && got != action) {
				t.Fatalf("%q: %v", action, data)
			}
		}
	}
}
