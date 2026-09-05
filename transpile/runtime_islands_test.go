package transpile_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/gsxcorpus"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/transpile"
)

type Row struct{ Label string }

func TestRuntimeIslandsGeneratedExecution(t *testing.T) {
	source := `package main
import "m31labs.dev/gosx/signal"
import authoredserver "m31labs.dev/gosx/server"
import authoredisland "m31labs.dev/gosx/island"
func AuthoredRuntime() *authoredserver.PageRuntime { return authoredserver.NewPageRuntime() }
func AuthoredSlots() authoredisland.ServerSlots { return nil }
type Row struct { Label string }
type PageProps struct { Label string; Rows []Row }
//gosx:island
component Panels() {
 visible := signal.New(true)
 toggle := func() { visible.Set(!visible.Get()) }
 return <div><button onClick={toggle}>Toggle</button><ServerSlot name="Profile" hidden={!visible.Get()} /></div>
}
component Wrapper(props: PageProps) {
 return <section><Panels><article slot="Profile"><input value={props.Label} /></article></Panels></section>
}
component Page(props: PageProps) {
 return <main><Each of={props.Rows} as="row"><Wrapper Label={row.Label} /></Each></main>
}
`
	output, err := transpile.Transpile([]byte(source), transpile.Options{RuntimeIslands: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := (gsxcorpus.Case{}).WriteStrictCheckModule(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "page.go"), []byte(output), 0600); err != nil {
		t.Fatal(err)
	}
	runner := `package main
import("crypto/sha256";"encoding/json";"fmt";"os";"m31labs.dev/gosx";"m31labs.dev/gosx/ir";"m31labs.dev/gosx/island/program";"m31labs.dev/gosx/server")
func main(){p,e:=gosx.Compile([]byte(` + strconv.Quote(source) + `));if e!=nil{panic(e)};ip,e:=ir.LowerIsland(p,0);if e!=nil{panic(e)};var results []string;for _,format:=range []string{"json","bin"}{rt:=server.NewPageRuntime();var b []byte;if format=="json"{b,e=program.EncodeJSON(ip)}else{b,e=program.EncodeBinary(ip)};if e!=nil{panic(e)};sum:=sha256.Sum256(b);hash:=fmt.Sprintf("%x",sum[:8]);rt.SetProgramAsset("Panels","/panels."+format,format,hash);props:=PageProps{Rows:[]Row{{Label:"A & B"},{Label:"A & B"}}};n,e:=Page(rt,props);if e!=nil{panic(e)};n2,e:=Page(rt,props);if e!=nil{panic(e)};if rt.Summary().Islands!=4{panic("missing registrations")};results=append(results,gosx.RenderHTML(gosx.Fragment(n,n2)),gosx.RenderHTML(rt.Head()))};json.NewEncoder(os.Stdout).Encode(results)}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(runner), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Dir = dir
	actual, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated execution: %v\n%s\n%s", err, actual, output)
	}
	var results []string
	if err := json.Unmarshal(actual, &results); err != nil {
		t.Fatal(err, string(actual))
	}
	if len(results) != 4 {
		t.Fatal(results)
	}
	p, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	ip, err := ir.LowerIsland(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, format := range []string{"json", "bin"} {
		var b []byte
		if format == "json" {
			b, err = program.EncodeJSON(ip)
		} else {
			b, err = program.EncodeBinary(ip)
		}
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		r := island.NewRenderer("gosx-page")
		r.SetRuntime("/gosx/runtime.wasm", "", 0)
		r.SetProgramAsset("Panels", "/panels."+format, format, fmt.Sprintf("%x", sum[:8]))
		env := route.ProgramRenderEnv{Props: struct {
			Label string
			Rows  []Row
		}{Rows: []Row{{"A & B"}, {"A & B"}}}, RenderIsland: r.RenderIslandFromProgram, RenderServerSlots: r.RenderWithServerSlots}
		html, err := route.RenderProgramComponent(p, "Page", env)
		if err != nil {
			t.Fatal(err)
		}
		html2, err := route.RenderProgramComponent(p, "Page", env)
		if err != nil {
			t.Fatal(err)
		}
		html += html2
		if html != results[i*2] {
			t.Fatalf("HTML parity: %s != %s", html, results[i*2])
		}
		head := gosx.RenderHTML(gosx.Fragment(r.PreloadHints(), r.PageHead()))
		if head != results[i*2+1] {
			t.Fatalf("manifest/asset parity: %s != %s", head, results[i*2+1])
		}
	}
	for i := 0; i < 4; i += 2 {
		if strings.Count(results[i], `value="A &amp; B"`) != 4 || strings.Count(results[i], `data-gosx-server-slot="Profile"`) != 4 {
			t.Fatal(results[i])
		}
		for _, want := range []string{`server-slots-v1`, `serverSlots`, `programRef`, `programHash`} {
			if !strings.Contains(results[i+1], want) {
				t.Fatalf("missing %s: %s", want, results[i+1])
			}
		}
	}
}

func TestRuntimeIslandsNoComponentsUnchanged(t *testing.T) {
	source := []byte("package app\nfunc Value() string { return \"plain\" }\n")
	plain, err := transpile.Transpile(source, transpile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	opt, err := transpile.Transpile(source, transpile.Options{RuntimeIslands: true})
	if err != nil {
		t.Fatal(err)
	}
	if plain != opt {
		t.Fatalf("unused runtime import/change: %s", opt)
	}
}
