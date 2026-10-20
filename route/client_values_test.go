package route

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
	islandprogram "m31labs.dev/gosx/island/program"
)

const clientCollectionSource = `package app
type Block struct { Name string; Active bool }
type Props struct { Initial []Block }
//gosx:island
component Collection(props: Props) {
 items := signal.New(props.Initial)
 clear := func() { items.Set([]Block{}) }
 return <section><button onClick={clear}>Clear</button><ul><Each of={items.Get()} as="item"><li data-active={item.Active}>{item.Name}</li></Each></ul></section>
}
func Page() Node {
 return <Collection {...data.collectionProps} />
}
`

var clientRecordMarshaled bool

type clientLoaderRecord struct {
	Name   string `json:"wrong-name"`
	Active bool
	Secret string
}

func (record clientLoaderRecord) MarshalJSON() ([]byte, error) {
	clientRecordMarshaled = true
	return []byte(`{"leak":"server-secret"}`), nil
}

func renderClientData(t *testing.T, source string, data any) (string, []byte, *vm.Island, error) {
	t.Helper()
	compiled, err := gosx.Compile([]byte(source))
	if err != nil {
		return "", nil, nil, err
	}
	// The file-program cache serializes IR. Exercise that boundary too.
	encoded, err := json.Marshal(compiled)
	if err != nil {
		t.Fatal(err)
	}
	var restored ir.Program
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	renderer := island.NewRenderer("test")
	renderer.SetBundle("test", "/gosx/runtime.wasm")
	renderer.SetRuntime("/gosx/runtime.wasm", "", 0)
	var payload []byte
	var live *vm.Island
	html, err := RenderProgramComponent(&restored, "Page", ProgramRenderEnv{
		Values: map[string]any{"data": map[string]any{"collectionProps": data}},
		RenderIsland: func(program *islandprogram.Program, props any) gosx.Node {
			var marshalErr error
			payload, marshalErr = json.Marshal(props)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			live = vm.NewIsland(program, string(payload))
			t.Cleanup(live.Dispose)
			return renderer.RenderIslandFromProgram(program, props)
		},
	})
	return html, payload, live, err
}

func TestClientCollectionPropsProjectLoaderData(t *testing.T) {
	clientRecordMarshaled = false
	data := struct {
		Initial []clientLoaderRecord
		Token   string
	}{[]clientLoaderRecord{{"Server coast 🌊", true, "server-secret"}}, "server-token"}
	html, payload, live, err := renderClientData(t, clientCollectionSource, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `>Server coast 🌊</li>`) || !strings.Contains(html, `data-active="true"`) {
		t.Fatalf("server snapshot did not use loader records: %s", html)
	}
	if clientRecordMarshaled || strings.Contains(string(payload), "secret") || strings.Contains(string(payload), "token") || strings.Contains(string(payload), "wrong-name") {
		t.Fatalf("client payload invoked JSON methods or exposed undeclared data: %s", payload)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 1 || decoded["Initial"] == nil {
		t.Fatalf("collection payload was duplicated under attribute aliases: %s", payload)
	}
	if live == nil {
		t.Fatal("island was not hydrated")
	}
	live.Dispatch("clear", `{}`)
	for _, node := range live.CurrentTree().Nodes {
		if node.Tag == "li" {
			t.Fatal("client could not clear server-loaded records")
		}
	}
}

func TestClientCollectionPropsProveEmptyAndNilTypes(t *testing.T) {
	for _, initial := range []any{[]int{}, []int(nil), []struct {
		Name   int
		Active bool
	}{}, []map[string]any{}} {
		_, _, _, err := renderClientData(t, clientCollectionSource, struct{ Initial any }{initial})
		if err == nil {
			t.Fatalf("invalid collection type %T crossed the island boundary", initial)
		}
	}
	for _, initial := range [][]clientLoaderRecord{nil, {}} {
		_, _, live, err := renderClientData(t, clientCollectionSource, struct{ Initial []clientLoaderRecord }{initial})
		if err != nil || live == nil {
			t.Fatalf("typed empty/nil collection rejected: %v", err)
		}
	}
}

func TestClientPropsProjectNestedReadPaths(t *testing.T) {
	source := `package app
type Profile struct { Name string; Token string }
type Props struct { Profile Profile }
//gosx:island
component Collection(props: Props) {
 name := signal.New(props.Profile.Name)
 return <p>{name.Get()}</p>
}
func Page() Node {
 return <Collection {...data.collectionProps} />
}`
	html, payload, _, err := renderClientData(t, source, struct{ Profile struct{ Name, Token string } }{struct{ Name, Token string }{"Public name", "server-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, ">Public name</p>") || strings.Contains(string(payload), "server-secret") {
		t.Fatalf("nested client data was not projected: %s / %s", html, payload)
	}
}

func TestClientPropsRecursiveTypesAndCyclicValues(t *testing.T) {
	source := `package app
type Branch struct { Name string; Children []Branch }
type Props struct { Initial []Branch }
//gosx:island
component Collection(props: Props) {
 items := signal.New(props.Initial)
 return <ul><Each of={items.Get()} as="item"><li>{item.Name}</li></Each></ul>
}

func Page() Node {
 return <Collection {...data.collectionProps} />
}`
	type branch struct {
		Name     string
		Children []branch
		Secret   string
	}
	items := []branch{{Name: "Root", Children: []branch{{Name: "Child", Secret: "server-secret"}}}}
	_, payload, _, err := renderClientData(t, source, struct{ Initial []branch }{items})
	if err != nil || !strings.Contains(string(payload), "Child") || strings.Contains(string(payload), "server-secret") {
		t.Fatalf("recursive records failed: %v / %s", err, payload)
	}
	items[0].Children = items
	_, _, _, err = renderClientData(t, source, struct{ Initial []branch }{items})
	if err == nil || !strings.Contains(err.Error(), "64 levels") {
		t.Fatalf("cyclic data was not bounded: %v", err)
	}
}

func TestClientPropsProjectSelectedMapKeys(t *testing.T) {
	source := `package app
type Props struct { Labels map[string]string }
//gosx:island
component Collection(props: Props) {
 label := signal.New(props.Labels["public"])
 return <p>{label.Get()}</p>
}
func Page() Node {
 return <Collection {...data.collectionProps} />
}`
	html, payload, _, err := renderClientData(t, source, struct{ Labels map[string]string }{map[string]string{"public": "Visible", "private": "server-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, ">Visible</p>") || strings.Contains(string(payload), "server-secret") {
		t.Fatalf("map projection failed: %s / %s", html, payload)
	}
}

func TestClientCollectionPropsKeepReferencedAttributeAlias(t *testing.T) {
	source := `package app
type Props struct { Initial []string }
//gosx:island
component Collection(props: Props) {
 items := signal.New(props.Initial)
 clear := func() { items.Set([]string{}) }
 return <section><button onClick={clear}>Clear</button><button data-on-click="items.Set(initial)">Reset</button><ul><Each of={items.Get()} as="item"><li>{item}</li></Each></ul></section>
}
func Page() Node {
 return <Collection {...data.collectionProps} />
}`
	_, payload, live, err := renderClientData(t, source, struct{ Initial []string }{[]string{"Retained"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"initial"`) {
		t.Fatalf("referenced legacy alias was removed: %s", payload)
	}
	live.Dispatch("clear", `{}`)
	var reset string
	for _, node := range live.CurrentTree().Nodes {
		for _, event := range node.Events {
			if event.Name == "click" && event.Handler != "clear" {
				reset = event.Handler
			}
		}
	}
	if reset == "" {
		t.Fatal("missing reset binding")
	}
	live.Dispatch(reset, `{}`)
	found := false
	tree := live.CurrentTree()
	for _, node := range tree.Nodes {
		if node.Tag == "li" && len(node.Children) > 0 && tree.Nodes[node.Children[0]].Text == "Retained" {
			found = true
		}
	}
	if !found {
		t.Fatal("legacy alias no longer resets the collection")
	}

}

func TestClientCollectionPropsRejectUnrepresentableSourceShapes(t *testing.T) {
	for _, declaration := range []string{
		`type Block struct { Name string; hidden string }`,
		`type Block struct { Name *string }`,
		`type Block struct { Name any }`,
		`type Block struct { Name string; Values map[int]string }`,
		`type Base struct { Name string }; type Block struct { Base }`,
	} {
		source := strings.Replace(clientCollectionSource, `type Block struct { Name string; Active bool }`, declaration, 1)
		if _, err := gosx.Compile([]byte(source)); err == nil {
			t.Fatalf("unsupported client source shape accepted: %s", declaration)
		}
	}
}
