package island

import (
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/hydrate"
)

func TestRendererBasic(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")

	if r.Manifest() == nil {
		t.Fatal("nil manifest")
	}
}

func TestRenderIsland(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")

	node := r.RenderIsland("Counter", map[string]int{"initial": 0}, gosx.Text("0"))
	html := gosx.RenderHTML(node)

	if !strings.Contains(html, "data-gosx-island") {
		t.Fatal("missing island attribute")
	}
	if !strings.Contains(html, "Counter") {
		t.Fatal("missing component name")
	}
}

func TestRenderIslandWithEvents(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")

	events := []hydrate.EventSlot{
		{SlotID: "s0", EventType: "click", HandlerName: "increment"},
	}

	node := r.RenderIslandWithEvents("Counter", map[string]int{"initial": 0}, events, gosx.Text("0"))
	html := gosx.RenderHTML(node)

	if !strings.Contains(html, "data-gosx-island") {
		t.Fatal("missing island attribute")
	}
}

func TestManifestScript(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")
	r.RenderIsland("Counter", nil, gosx.Text("0"))

	script := r.ManifestScript()
	html := gosx.RenderHTML(script)

	if !strings.Contains(html, "gosx-manifest") {
		t.Fatal("missing manifest script tag")
	}
}

func TestPageHeadEmpty(t *testing.T) {
	r := NewRenderer("main")
	// No islands rendered — PageHead should return empty
	head := r.PageHead()
	html := gosx.RenderHTML(head)
	if html != "" {
		t.Fatalf("expected empty for no islands, got %q", html)
	}
}

func TestPageHeadWithIslands(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")
	r.RenderIsland("Counter", nil, gosx.Text("0"))

	head := r.PageHead()
	html := gosx.RenderHTML(head)

	if !strings.Contains(html, "gosx-manifest") {
		t.Fatal("missing manifest in PageHead")
	}
	if !strings.Contains(html, "bootstrap.js") {
		t.Fatal("missing bootstrap script in PageHead")
	}
}

func TestChecksum(t *testing.T) {
	sum1 := Checksum([]byte("hello"))
	sum2 := Checksum([]byte("hello"))
	sum3 := Checksum([]byte("world"))

	if sum1 != sum2 {
		t.Fatal("same input should produce same checksum")
	}
	if sum1 == sum3 {
		t.Fatal("different input should produce different checksum")
	}
}

func TestSerializeProps(t *testing.T) {
	data, err := SerializeProps(map[string]int{"count": 5})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "count") {
		t.Fatalf("expected 'count' in serialized props, got %q", string(data))
	}
}

func TestSerializePropsInvalid(t *testing.T) {
	_, err := SerializeProps(make(chan int))
	if err == nil {
		t.Fatal("expected error for non-serializable props")
	}
}

func TestManifestJSON(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")
	r.RenderIsland("Counter", map[string]int{"initial": 0}, gosx.Text("0"))

	jsonStr, err := r.ManifestJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonStr, "Counter") {
		t.Fatalf("expected 'Counter' in manifest JSON, got %q", jsonStr)
	}
}
