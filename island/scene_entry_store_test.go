package island

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestSceneEntryStorePutIsContentAddressed(t *testing.T) {
	s := NewSceneEntryStore(8, 1<<20)
	a := s.Put([]byte(`{"scene":1}`))
	b := s.Put([]byte(`{"scene":1}`))
	c := s.Put([]byte(`{"scene":2}`))
	if a != b || a == c || len(a) != 32 || !ValidSceneEntryHash(a) {
		t.Fatalf("hashes a=%s b=%s c=%s", a, b, c)
	}
	if got, ok := s.Get(a); !ok || string(got) != `{"scene":1}` {
		t.Fatalf("get = %q %v", got, ok)
	}
	if _, ok := s.Get("0000"); ok {
		t.Fatal("unknown hash must miss")
	}
	if SceneEntryPath(a) != "/gosx/scene3d/"+a+".json" {
		t.Fatal(SceneEntryPath(a))
	}
	if ValidSceneEntryHash("../x") || ValidSceneEntryHash("ABC") {
		t.Fatal("only lowercase hex of the right length is a hash")
	}
}

func TestSceneEntryStoreEvictsLeastRecentlyUsed(t *testing.T) {
	s := NewSceneEntryStore(2, 1<<20)
	a := s.Put([]byte("a"))
	b := s.Put([]byte("b"))
	s.Get(a) // a is now the most recent
	s.Put([]byte("c"))
	if _, ok := s.Get(b); ok {
		t.Fatal("b must be evicted first")
	}
	if _, ok := s.Get(a); !ok {
		t.Fatal("a must survive")
	}
	tiny := NewSceneEntryStore(8, 3)
	x := tiny.Put([]byte("xx"))
	tiny.Put([]byte("yy"))
	if _, ok := tiny.Get(x); ok {
		t.Fatal("byte budget must evict x")
	}
}

func TestDefaultSceneEntryStoreCanBeDisabled(t *testing.T) {
	prev := DefaultSceneEntryStore()
	t.Cleanup(func() { SetSceneEntryStore(prev) })
	SetSceneEntryStore(nil)
	if DefaultSceneEntryStore() != nil {
		t.Fatal("nil must disable the store")
	}
}

func TestSceneEntryStoreIsOffDuringStaticExport(t *testing.T) {
	env := func(v string) func(string) string {
		return func(name string) string {
			if name == "GOSX_STATIC_EXPORT" {
				return v
			}
			return ""
		}
	}
	if sceneEntryStoreFromEnv(env("1")) != nil {
		t.Fatal("gosx export sets GOSX_STATIC_EXPORT=1; a static site has no server for /gosx/scene3d/, so props stay inline")
	}
	if sceneEntryStoreFromEnv(env("")) == nil || sceneEntryStoreFromEnv(env("0")) == nil {
		t.Fatal("a running server keeps the store")
	}
}

func TestStaticExportKeepsDeferredPropsInline(t *testing.T) {
	prev := DefaultSceneEntryStore()
	t.Cleanup(func() { SetSceneEntryStore(prev) })
	SetSceneEntryStore(sceneEntryStoreFromEnv(func(string) string { return "1" }))
	r := NewRenderer("main")
	r.RenderEngine(deferredSceneConfig("hero"), gosx.Text("poster"))
	manifest, _ := r.ManifestJSON()
	if strings.Contains(manifest, "propsRef") || strings.Contains(manifest, `"props":null`) || !strings.Contains(manifest, `/models/hero.glb`) {
		t.Fatalf("exported pages must carry their props inline: %s", manifest)
	}
}
