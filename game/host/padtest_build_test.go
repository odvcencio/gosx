//go:build !js || !wasm

package host_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func wasmEnv() []string { return append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off") }

// Both variants of the browser host compile and vet. The tag decides whether
// the pad test adapter exists.
func TestPadTestBuildVariantsVet(t *testing.T) {
	for _, tags := range []string{"", "gosx_padtest"} {
		cmd := exec.Command("go", "vet", "-tags", tags, ".")
		cmd.Env = wasmEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("go vet -tags %q: %v\n%s", tags, err, out)
		}
	}
}

// A page built without the tag holds no pad test parser and no connector, so
// the query field text is not in the file. With the tag it is. The program is
// hosttest/testdata/padpage, which imports the reader and the frame source.
func TestPadTestStringsOnlyInTaggedWasm(t *testing.T) {
	const program = "./hosttest/testdata/padpage"
	needles := []string{"gosx-padtest", "data-padtest", "pad test endpoint", "pad test port", "pad test token", "PadTestSource", "PadTestSession", "padtest"}
	for _, tagged := range []bool{false, true} {
		args := []string{"build", "-o", filepath.Join(t.TempDir(), "page.wasm")}
		if tagged {
			args = append(args, "-tags", "gosx_padtest")
		}
		out := args[2]
		cmd := exec.Command("go", append(args, program)...)
		cmd.Env = wasmEnv()
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
