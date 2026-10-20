package docs

import (
	"os"
	"regexp"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

func TestDocumentedCapabilityTableMatchesSourceMatrix(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}

	rowPattern := regexp.MustCompile(`(?s)<tr>\s*<th scope="row">([^<]+)</th>\s*<td>(yes|no)</td>\s*<td>(yes|no)</td>\s*<td>(yes|no)</td>\s*<td>(yes|no)</td>\s*</tr>`)
	rows := map[capability.Feature][4]bool{}
	for _, match := range rowPattern.FindAllSubmatch(source, -1) {
		feature := capability.Feature(match[1])
		rows[feature] = [4]bool{
			string(match[2]) == "yes",
			string(match[3]) == "yes",
			string(match[4]) == "yes",
			string(match[5]) == "yes",
		}
	}

	if len(rows) != len(capability.Matrix) {
		t.Fatalf("documented capability rows = %d, source matrix rows = %d", len(rows), len(capability.Matrix))
	}
	policy := capability.DefaultPolicy()
	for feature := range capability.Matrix {
		got, ok := rows[feature]
		if !ok {
			t.Errorf("capability table is missing %q", feature)
			continue
		}
		want := [4]bool{
			capability.Supports(capability.BackendWebGPU, feature),
			capability.Supports(capability.BackendWebGL, feature),
			capability.Supports(capability.BackendCanvas2D, feature),
			policy.Required[feature],
		}
		if got != want {
			t.Errorf("capability row %q = %v, want WebGPU/WebGL2/Canvas2D/Required %v", feature, got, want)
		}
	}
}
