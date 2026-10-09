//go:build !tinygo

package ir_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

func TestIslandAOTNativeGoValues(t *testing.T) {
	expressions := []string{"props.Value+1", "1+props.Value", "props.I32*2", "2*props.I32", "props.Label+\"!\"", "len(props.Label)", "props.Flag && true", "props.Value<10", "props.Detail.Label", "-2147483648", "2147483647", "count.Get()+1"}
	declarations := `type Detail struct{ Label string }; type Props struct{Value int; I32 int32; Label string; Flag bool; Detail Detail}`
	var values strings.Builder
	for _, expression := range expressions {
		fmt.Fprintf(&values, "fmt.Sprint(%s),\n", expression)
	}
	native := "package main\nimport (\"encoding/json\"; \"fmt\"; \"os\"; signal \"m31labs.dev/gosx/signal\")\n" + declarations + `
func main(){ props:=Props{7,-3,"héllo",true,Detail{"ready"}}; count:=signal.New[int32](2); json.NewEncoder(os.Stdout).Encode([]string{` + values.String() + "}) }"
	dir := t.TempDir()
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"main.go": native, "go.mod": "module example.test/nativecheck\n\ngo 1.26\nrequire m31labs.dev/gosx v0.0.0\nreplace m31labs.dev/gosx => " + filepath.ToSlash(repo) + "\n"}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command("go", "run", "-mod=mod", ".")
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("compiled Go: %v\n%s", err, output)
	}
	var expected []string
	if err := json.Unmarshal(output, &expected); err != nil {
		t.Fatal(err)
	}
	for i, expression := range expressions {
		t.Run("native-Go/"+expression, func(t *testing.T) {
			source := "package example\nimport signal \"m31labs.dev/gosx/signal\"\n" + declarations + "\n//gosx:island\nfunc Counter(props Props) Node {\n count:=signal.New[int32](2); _=count;\n return <div>{" + expression + "}</div>\n}\n"
			p, err := parseAOT(t, []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			p.PackagePath = "example/components"
			u, err := ir.LowerIslandAOT(p, 0)
			if err != nil {
				t.Fatal(err)
			}
			props := map[string]vm.Value{"Value": vm.IntVal(7), "I32": vm.IntVal(-3), "Label": vm.StringVal("héllo"), "Flag": vm.BoolVal(true), "Detail": vm.ObjectVal(map[string]vm.Value{"Label": vm.StringVal("ready")})}
			propsObject := make(map[string]vm.Value, len(props))
			for name, value := range props {
				propsObject[name] = value
			}
			props["props"] = vm.ObjectVal(propsObject)
			machine := vm.NewVM(u.Program, props)
			vm.InitSignals(machine, u.Program)
			t.Cleanup(func() { machine.SwapProgram(&program.Program{}) })
			id := u.Program.Nodes[u.Program.Nodes[u.Program.Root].Children[0]].Expr
			if got := machine.Eval(id).String(); got != expected[i] {
				t.Fatalf("VM=%q compiled Go=%q", got, expected[i])
			}
		})
	}
}
