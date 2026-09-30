package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPropsRenderBeforeModelsSerializes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value *bool
		want  string
	}{
		{name: "unset"},
		{name: "enabled", value: Bool(true), want: `"renderBeforeModels":true`},
		{name: "disabled", value: Bool(false), want: `"renderBeforeModels":false`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			props := Props{RenderBeforeModels: tc.value}
			for _, value := range []any{props, props.LegacyProps()} {
				wire, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if tc.want == "" {
					if strings.Contains(string(wire), `"renderBeforeModels"`) {
						t.Fatalf("unset prop must be omitted: %s", wire)
					}
				} else if !strings.Contains(string(wire), tc.want) {
					t.Fatalf("wire missing %s: %s", tc.want, wire)
				}
			}
		})
	}
}
