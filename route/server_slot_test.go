package route

import (
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/transpile"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerSlotCallerSSR(t *testing.T) {
	source := []byte(`package app
import "m31labs.dev/gosx/signal"
type PanelsProps struct { Instance string }
//gosx:island
component Panels(props: PanelsProps) {
 selected := signal.New("profile")
 show := func() { selected.Set("security") }
 return <div>
  <button onClick={show}>Select</button>
  <ServerSlot name="Profile" id={props.Instance + "-profile"} role="tabpanel" aria-labelledby={props.Instance + "-trigger"} hidden={selected.Get() != "profile"} />
  <ServerSlot name="Security" hidden={selected.Get() != "security"} />
 </div>
}

component Page() {
 return <main><Panels Instance="a"><section slot="Profile"><input value="A &amp; B" /></section><section slot="Security">Secret &amp; safe</section></Panels></main>
}
`)
	p, err := gosx.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	r := island.NewRenderer("test")
	output, err := RenderProgramComponent(p, "Page", ProgramRenderEnv{RenderIsland: r.RenderIslandFromProgram, RenderServerSlots: r.RenderWithServerSlots})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-gosx-server-slot="Profile"`, `id="a-profile"`, `<input`, `Secret &amp; safe`} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %s: %s", want, output)
		}
	}
	if strings.Contains(output, " hidden") || strings.Contains(output, "</input>") {
		t.Fatalf("invalid fallback: %s", output)
	}
	if len(r.Manifest().Islands) != 1 || len(r.Manifest().Islands[0].ServerSlots) != 2 {
		t.Fatalf("manifest: %+v", r.Manifest().Islands)
	}
}

func TestServerSlotRejectsNestedOwnerBeforeRendering(t *testing.T) {
	source := `package app
//gosx:island
component Inner() {
 return <button>Inner</button>
}
component Wrapper() {
 return <section><Inner /></section>
}
//gosx:island
component Panels() {
 return <div><ServerSlot name="Profile" /></div>
}
component Page() {
 return <Panels><section slot="Profile"><Wrapper /></section></Panels>
}
`
	_, err := gosx.Compile([]byte(source))
	if err == nil || !strings.Contains(err.Error(), "ServerSlot Profile cannot contain nested owner Inner") {
		t.Fatalf("expected pre-render ownership rejection, got %v", err)
	}
	output, err := transpile.Transpile([]byte(source), transpile.Options{RuntimeIslands: true})
	if err == nil || output != "" || !strings.Contains(err.Error(), "nested owner Inner") {
		t.Fatalf("generated caller must refuse before emission: %q %v", output, err)
	}
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", source)
	router := NewRouter()
	if err := router.AddDir(root, FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	router.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 500 {
		t.Fatalf("nested owner file response = %d %s", w.Code, w.Body.String())
	}
	for _, forbidden := range []string{"data-gosx-island", "serverSlotHost", "gosx-manifest", "server-slots.js"} {
		if strings.Contains(w.Body.String(), forbidden) {
			t.Fatalf("failed response published %s: %s", forbidden, w.Body.String())
		}
	}
}
