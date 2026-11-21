//go:build !js || !wasm

package loop_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A page built without the gosx_capture tag carries none of the capture clock:
// the attribute names are not in the file. With the tag they are.
func TestCaptureClockOnlyInTaggedWasm(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two wasm programs")
	}
	needles := []string{"gosxCaptureStep", "gosxCaptureGrant", "gosxCaptureDone"}
	for _, tagged := range []bool{false, true} {
		out := filepath.Join(t.TempDir(), "page.wasm")
		args := []string{"build", "-o", out}
		if tagged {
			args = append(args, "-tags", "gosx_capture")
		}
		cmd := exec.Command("go", append(args, "./testdata/capturepage")...)
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off")
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build (tagged=%v): %v\n%s", tagged, err, msg)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range needles {
			if found := bytes.Contains(data, []byte(needle)); found != tagged {
				t.Errorf("tagged=%v: %q present=%v, want %v", tagged, needle, found, tagged)
			}
		}
	}
}
