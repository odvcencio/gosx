package docs

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestClipperSailsFromAuthoredMooring(t *testing.T) {
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"vessel": blackglassBeachVessel(), "walk": blackglassBeachWalk(), "ocean": map[string]any{"level": 0}})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", filepath.Join(root, "client/js/testdata/vessel-mooring.cjs"), root)
	cmd.Stdin = bytes.NewReader(data)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mooring simulation: %v\n%s", err, output)
	}
	t.Logf("%s", output)
}
