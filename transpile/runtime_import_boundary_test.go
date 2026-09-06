package transpile

import (
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	"strings"
	"testing"
)

func TestRuntimeImportedOwnershipDiscovery(t *testing.T) {
	owner := `//gosx:island
component Panels() {
 return <div><ServerSlot name="Body" /></div>
}
`
	local := "package app\n" + owner + `component Page() {
 return <Panels><article slot="Body">preserved</article></Panels>
}
`
	out, err := Transpile([]byte(local), Options{RuntimeIslands: true})
	if err != nil || !strings.Contains(out, ".IslandJSON(") || !strings.Contains(out, "preserved") {
		t.Fatalf("local ownership: %v\n%s", err, out)
	}
	shared := CollectSharedComponents([]PackageFile{{Source: []byte("package ui\n" + owner)}})
	if _, ok := shared["Panels"]; ok {
		t.Fatal("runtime owner unexpectedly represented in projection metadata")
	}
	caller := `package app
import ui "./ui"
component Page() {
 return <ui.Panels><article slot="Body">preserved</article></ui.Panels>
}
`
	_, err = Transpile([]byte(caller), Options{RuntimeIslands: true, SourceFile: "page.gsx", SharedImports: map[string]SharedImport{"./ui": {GoImportPath: "example.test/ui", Components: shared}}})
	if err == nil {
		t.Fatal("expected imported owner refusal")
	}
	t.Logf("current imported ownership diagnostic: %v", err)
	if !strings.Contains(err.Error(), "run gosx check") {
		t.Fatal("missing-metadata compatibility changed", err)
	}
}

func TestSharedRuntimeOwnerDiagnostics(t *testing.T) {
	for _, kind := range []SharedRuntimeOwner{SharedIslandOwner, SharedEngineOwner} {
		for _, call := range []string{"<ui.Panels Label={props.Label} />", "<ui.Panels Label={props.Label}><b>body</b></ui.Panels>", "<ui.Panels {...props} />", "<ui.Panels {...props}><b>body</b></ui.Panels>", "<ui.Panels><article slot=\"Body\">preserved</article></ui.Panels>"} {
			src := "package app\nimport ui \"./ui\"\ntype Props struct { Label string }\ncomponent Page(props: Props) {\n return " + call + "\n}\n"
			_, err := Transpile([]byte(src), Options{RuntimeIslands: true, SharedImports: map[string]SharedImport{"./ui": {GoImportPath: "example.test/ui", Components: map[string]SharedComponent{"Panels": {PropsType: "Props", Fields: map[string]string{"Label": "Label"}, AcceptsChildren: true, AcceptsSlots: []string{"Body"}}}, RuntimeOwners: map[string]SharedRuntimeOwner{"Panels": kind}}}})
			if err == nil || !strings.Contains(err.Error(), "5:9: imported "+string(kind)+" component ui.Panels") || strings.Contains(err.Error(), "run gosx check") {
				t.Fatalf("%s %s: %v", kind, call, err)
			}
			if strings.Contains(err.Error(), "same-file") != (kind == SharedIslandOwner) {
				t.Fatalf("untruthful owner guidance: %v", err)
			}
		}
	}
}

func TestSharedRuntimeOwnerCollection(t *testing.T) {
	file := func(directive string) PackageFile {
		return PackageFile{Source: []byte("package ui\n" + directive + "\ncomponent Card() {\n return <div />\n}\n")}
	}
	pure, island, engine := file(""), file("//gosx:island"), file("//gosx:engine worker")
	// Strict engine source is currently rejected upstream. Exercise the
	// already-loaded metadata contract without widening that admission gate.
	engine.Program = &ir.Program{Components: []ir.Component{{Name: "Card", Syntax: ir.ComponentSyntaxStrict, IsEngine: true}}}
	for _, tc := range []struct {
		file PackageFile
		kind SharedRuntimeOwner
	}{{island, SharedIslandOwner}, {engine, SharedEngineOwner}} {
		if got := CollectSharedRuntimeOwners([]PackageFile{tc.file}); got["Card"] != tc.kind {
			_, err := gosx.Compile(tc.file.Source)
			t.Fatalf("source collection %s: %v compile=%v", tc.kind, got, err)
		}
	}
	for _, files := range [][]PackageFile{{pure, island}, {island, pure}, {engine, island}, {island, island, island}} {
		if got := CollectSharedRuntimeOwners(files); len(got) != 0 {
			t.Fatalf("ambiguous owner: %v", got)
		}
	}
}

func TestStrictSpreadPayloadEmission(t *testing.T) {
	src := `package app
type Props struct { Label string }
component Card(props: Props) {
 return <section>{slotHeader}{children}<b>{props.Label}</b></section>
}

component Page(props: Props) {
 return <Card {...props}><p>child</p><h1 slot="Header">heading</h1></Card>
}
`
	for _, runtime := range []bool{false, true} {
		out, err := Transpile([]byte(src), Options{RuntimeIslands: runtime})
		if err != nil {
			t.Fatal(err)
		}
		call := `Card(props, gosx.El("h1", gosx.Text("heading")), gosx.El("p", gosx.Text("child")))`
		if runtime {
			call = strings.Replace(call, "Card(props", "__gosxRuntimeCard(__gosxRuntimectx, props", 1)
		}
		if !strings.Contains(out, call) {
			t.Fatalf("missing ordered payload call %s: %s", call, out)
		}
	}
}

func TestStrictSpreadSelfClosingSlotEmission(t *testing.T) {
	src := `package app
type Props struct { Label string }
component Card(props: Props) {
 return <section>{slotHeader}<b>{props.Label}</b></section>
}
component Page(props: Props) {
 return <Card {...props} />
}
`
	for _, runtime := range []bool{false, true} {
		out, err := Transpile([]byte(src), Options{RuntimeIslands: runtime})
		if err != nil {
			t.Fatal(err)
		}
		call := "Card(props, gosx.Fragment())"
		if runtime {
			call = "__gosxRuntimeCard(__gosxRuntimectx, props, gosx.Fragment())"
		}
		if !strings.Contains(out, call) {
			t.Fatalf("missing self-closing ordered slot call %s: %s", call, out)
		}
	}
}
