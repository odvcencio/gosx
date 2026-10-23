package server

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/scene"
)

var testSceneSecret = []byte("0123456789abcdef-test-secret")

type refillFixture struct {
	handler http.Handler
	renders atomic.Int64
	// gate, when set, blocks /slow renders until it is closed or the request ends.
	gate chan struct{}
	// ctxDone is closed when a /slow render sees its request context end.
	ctxDone chan struct{}
	entered chan struct{}
}

func newRefillFixture(t *testing.T, secret []byte, scenes int) *refillFixture {
	t.Helper()
	prev := island.DefaultSceneEntryStore()
	t.Cleanup(func() { island.SetSceneEntryStore(prev) })
	f := &refillFixture{ctxDone: make(chan struct{}, 16), entered: make(chan struct{}, 64)}
	app := New()
	if secret != nil {
		app.SetSceneEntrySecret(secret)
	}
	one := func(ctx *Context, id string, public bool, label string) gosx.Node {
		cfg := scene.Props{
			Label: label, Defer: &scene.Defer{Public: public},
			Graph: scene.NewGraph(scene.Model{ID: "m", Src: "/models/" + id + ".glb"}),
		}.EngineConfig()
		cfg.MountID = id
		return ctx.Runtime().Engine(cfg, gosx.Text("poster"))
	}
	app.Page("/scene", func(ctx *Context) gosx.Node {
		f.renders.Add(1)
		return one(ctx, "hero", true, "")
	})
	app.Page("/slow", func(ctx *Context) gosx.Node {
		f.renders.Add(1)
		f.entered <- struct{}{}
		if f.gate != nil {
			select {
			case <-f.gate:
			case <-ctx.Request.Context().Done():
				f.ctxDone <- struct{}{}
			case <-time.After(5 * time.Second):
			}
		}
		return one(ctx, "slow", true, "")
	})
	// Two routes that render the same layout-level scene: one hash, two routes.
	for _, route := range []string{"/a", "/b"} {
		app.Page(route, func(ctx *Context) gosx.Node {
			f.renders.Add(1)
			f.entered <- struct{}{}
			if f.gate != nil {
				select {
				case <-f.gate:
				case <-ctx.Request.Context().Done():
				case <-time.After(5 * time.Second):
				}
			}
			return one(ctx, "shared", true, "")
		})
	}
	app.Page("/many", func(ctx *Context) gosx.Node {
		f.renders.Add(1)
		var nodes []gosx.Node
		for i := 0; i < scenes; i++ {
			nodes = append(nodes, one(ctx, fmt.Sprintf("hero-%d", i), true, ""))
		}
		return gosx.Fragment(nodes...)
	})
	// A page whose props depend on the visitor and never opted in to a public URL.
	app.Page("/private", func(ctx *Context) gosx.Node {
		return one(ctx, "hero", false, "secret for "+ctx.Request.Header.Get("Cookie"))
	})
	app.Page("/boom", func(ctx *Context) gosx.Node {
		f.renders.Add(1)
		panic("render exploded")
	})
	app.Page("/abort", func(ctx *Context) gosx.Node {
		panic(http.ErrAbortHandler)
	})
	f.handler = app.Build()
	return f
}

var propsRefRe = regexp.MustCompile(`"propsRef":"([^"]+)"`)

func (f *refillFixture) refs(t *testing.T, target string) []string {
	t.Helper()
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	var out []string
	for _, m := range propsRefRe.FindAllStringSubmatch(w.Body.String(), -1) {
		out = append(out, strings.ReplaceAll(html.UnescapeString(m[1]), `\u0026`, "&"))
	}
	return out
}

func (f *refillFixture) get(ref string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, ref, nil))
	return w
}

func refHash(ref string) string {
	return strings.TrimSuffix(strings.TrimPrefix(strings.SplitN(ref, "?", 2)[0], island.SceneEntryPathPrefix), ".json")
}

func signedRef(hash, route string) string {
	return island.SceneEntryPath(hash) + "?r=" + url.QueryEscape(route) + "&s=" + island.SignSceneEntry(testSceneSecret, route, hash)
}

func assertNoStore(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d Cache-Control=%q, want %d no-store", w.Code, w.Header().Get("Cache-Control"), status)
	}
}

// A page rendered by one process names a props hash another process never
// stored. The signed URL lets this process render the route and serve the same
// bytes: one render, immutable cache header.
func TestSceneEntryMissRefillsFromSignedRoute(t *testing.T) {
	storeA := island.NewSceneEntryStore(8, 1<<20)
	island.SetSceneEntryStore(storeA)
	f := newRefillFixture(t, testSceneSecret, 0)
	refs := f.refs(t, "/scene")
	if len(refs) != 1 || !strings.Contains(refs[0], "?r=%2Fscene&s=") {
		t.Fatalf("props URL must carry its signed route: %v", refs)
	}
	want, _ := storeA.Get(refHash(refs[0]))

	island.SetSceneEntryStore(island.NewSceneEntryStore(8, 1<<20)) // another process
	f.renders.Store(0)
	w := f.get(refs[0])
	if w.Code != 200 || w.Body.String() != string(want) || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("refill = %d CC=%q equal=%v", w.Code, w.Header().Get("Cache-Control"), w.Body.String() == string(want))
	}
	if n := f.renders.Load(); n != 1 {
		t.Fatalf("renders = %d, want 1", n)
	}
	// The bytes are stored now: no second render.
	if w := f.get(refs[0]); w.Code != 200 || f.renders.Load() != 1 {
		t.Fatalf("second request = %d, renders=%d", w.Code, f.renders.Load())
	}
}

func TestSceneEntryRefillNeedsASignedRoute(t *testing.T) {
	island.SetSceneEntryStore(island.NewSceneEntryStore(8, 1<<20))
	f := newRefillFixture(t, testSceneSecret, 0)
	hash := strings.Repeat("ab", 16)
	good := signedRef(hash, "/scene")
	cases := map[string]string{
		"no route":          island.SceneEntryPath(hash),
		"route, no sig":     island.SceneEntryPath(hash) + "?r=%2Fscene",
		"forged sig":        island.SceneEntryPath(hash) + "?r=%2Fscene&s=" + strings.Repeat("0", 32),
		"sig for a route":   island.SceneEntryPath(hash) + "?r=%2Fslow&s=" + island.SignSceneEntry(testSceneSecret, "/scene", hash),
		"sig for a hash":    island.SceneEntryPath(strings.Repeat("cd", 16)) + good[strings.Index(good, "?"):],
		"framework route":   signedRef(hash, "/gosx/scene3d/x.json"),
		"protocol-relative": signedRef(hash, "//evil.example"),
	}
	for name, ref := range cases {
		f.renders.Store(0)
		if w := f.get(ref); w.Code != 404 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: status=%d CC=%q ref=%s body=%q", name, w.Code, w.Header().Get("Cache-Control"), ref, w.Body.String())
		}
		if f.renders.Load() != 0 {
			t.Fatalf("%s: rendered %d times", name, f.renders.Load())
		}
	}
	// A correctly signed route that does not produce the hash: one render, then 404 without more renders.
	f.renders.Store(0)
	for i := 0; i < 5; i++ {
		assertNoStore(t, f.get(good), http.StatusNotFound)
	}
	if n := f.renders.Load(); n != 1 {
		t.Fatalf("a remembered miss rendered %d times, want 1", n)
	}
}

func TestSceneEntryWithoutSecretNamesNoRouteAndNeverRefills(t *testing.T) {
	island.SetSceneEntryStore(island.NewSceneEntryStore(8, 1<<20))
	t.Setenv("GOSX_SCENE_ENTRY_SECRET", "")
	f := newRefillFixture(t, nil, 0)
	refs := f.refs(t, "/scene")
	if len(refs) != 1 || strings.Contains(refs[0], "?") {
		t.Fatalf("without a secret the URL names no route: %v", refs)
	}
	island.SetSceneEntryStore(island.NewSceneEntryStore(8, 1<<20))
	f.renders.Store(0)
	assertNoStore(t, f.get(refs[0]), http.StatusNotFound)
	if f.renders.Load() != 0 {
		t.Fatal("a miss must not render without a secret")
	}
}

// Props that depend on the visitor stay in the page unless the scene says it
// is Public. A cookie-dependent page must not put them behind a shared URL.
func TestSceneEntryKeepsNonPublicPropsInline(t *testing.T) {
	store := island.NewSceneEntryStore(8, 1<<20)
	island.SetSceneEntryStore(store)
	f := newRefillFixture(t, testSceneSecret, 0)
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Cookie", "sid=alice")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	body := w.Body.String()
	if strings.Contains(body, "propsRef") || !strings.Contains(body, "secret for sid=alice") {
		t.Fatalf("non-public props must stay inline: %s", body)
	}
	if _, ok := store.Get(island.SceneEntryHash([]byte(`{}`))); ok {
		t.Fatal("store must be empty")
	}
	anon := httptest.NewRecorder()
	f.handler.ServeHTTP(anon, httptest.NewRequest(http.MethodGet, "/private", nil))
	if strings.Contains(anon.Body.String(), "alice") {
		t.Fatal("anonymous page shows alice's data")
	}
	for _, ref := range propsRefRe.FindAllString(body, -1) {
		t.Fatalf("unexpected props URL %s", ref)
	}
}

func TestSceneEntrySingleflightPerKey(t *testing.T) {
	island.SetSceneEntryStore(island.NewSceneEntryStore(8, 1<<20))
	f := newRefillFixture(t, testSceneSecret, 0)
	f.gate = make(chan struct{})
	refs := f.refs2(t, "/slow", f.gate)
	hash := refHash(refs[0])
	island.SetSceneEntryStore(island.NewSceneEntryStore(8, 1<<20))
	f.renders.Store(0)
	f.gate = make(chan struct{})
	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = f.get(refs[0]).Code
		}(i)
	}
	<-f.entered // the leader is rendering
	time.Sleep(50 * time.Millisecond)
	close(f.gate)
	wg.Wait()
	for i, c := range codes {
		if c != 200 {
			t.Fatalf("request %d = %d", i, c)
		}
	}
	if n := f.renders.Load(); n != 1 {
		t.Fatalf("8 requests for hash %s rendered %d times, want 1", hash, n)
	}
}

// refs2 renders target with its gate open, then restores a blocking gate.
func (f *refillFixture) refs2(t *testing.T, target string, gate chan struct{}) []string {
	t.Helper()
	close(gate)
	defer func() { f.gate = nil }()
	return f.refs(t, target)
}

func TestSceneEntryRefillConcurrencyCap(t *testing.T) {
	island.SetSceneEntryStore(island.NewSceneEntryStore(64, 1<<20))
	f := newRefillFixture(t, testSceneSecret, 0)
	f.gate = make(chan struct{})
	var wg sync.WaitGroup
	const n = 10
	codes := make([]int, n)
	headers := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			hash := fmt.Sprintf("%032x", i+1)
			w := f.get(signedRef(hash, "/slow"))
			codes[i], headers[i] = w.Code, w.Header().Get("Cache-Control")
		}(i)
	}
	for i := 0; i < sceneRefillMaxFlight; i++ {
		<-f.entered
	}
	time.Sleep(100 * time.Millisecond)
	if got := f.renders.Load(); got != sceneRefillMaxFlight {
		t.Fatalf("renders in flight = %d, want %d", got, sceneRefillMaxFlight)
	}
	close(f.gate)
	wg.Wait()
	busy := 0
	for i, c := range codes {
		if c == http.StatusServiceUnavailable {
			busy++
			if headers[i] != "no-store" {
				t.Fatalf("503 Cache-Control = %q", headers[i])
			}
		}
	}
	if busy != n-sceneRefillMaxFlight {
		t.Fatalf("busy = %d, want %d (codes %v)", busy, n-sceneRefillMaxFlight, codes)
	}
}

// A client that leaves ends the render it started and frees the slot, and a
// waiter that leaves does not block on the leader.
func TestSceneEntryRefillStopsWhenTheClientLeaves(t *testing.T) {
	island.SetSceneEntryStore(island.NewSceneEntryStore(8, 1<<20))
	f := newRefillFixture(t, testSceneSecret, 0)
	f.gate = make(chan struct{})
	ref := signedRef(strings.Repeat("ee", 16), "/slow")

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		req := httptest.NewRequest(http.MethodGet, ref, nil).WithContext(leaderCtx)
		f.handler.ServeHTTP(httptest.NewRecorder(), req)
	}()
	<-f.entered

	waiterCtx, cancelWaiter := context.WithCancel(context.Background())
	waiterDone := make(chan struct{})
	waiterResp := httptest.NewRecorder()
	go func() {
		defer close(waiterDone)
		req := httptest.NewRequest(http.MethodGet, ref, nil).WithContext(waiterCtx)
		f.handler.ServeHTTP(waiterResp, req)
	}()
	time.Sleep(50 * time.Millisecond)
	cancelWaiter()
	select {
	case <-waiterDone:
	case <-time.After(2 * time.Second):
		t.Fatal("a waiter whose client left must return")
	}
	// net/http would send an empty 200 for a handler that wrote nothing.
	assertNoStore(t, waiterResp, http.StatusServiceUnavailable)

	cancelLeader()
	select {
	case <-f.ctxDone:
	case <-time.After(2 * time.Second):
		t.Fatal("the render must see the request context end")
	}
	select {
	case <-leaderDone:
	case <-time.After(2 * time.Second):
		t.Fatal("the leader must return")
	}
	// The slot is free again.
	f.gate = nil
	w := f.get(signedRef(strings.Repeat("ff", 16), "/slow"))
	if w.Code == http.StatusServiceUnavailable {
		t.Fatal("slot leaked after the client left")
	}
}

// A page with more deferred scenes than the store holds still serves each one.
func TestSceneEntryRefillSurvivesEvictionOnBigPages(t *testing.T) {
	island.SetSceneEntryStore(island.NewSceneEntryStore(8, 1<<22))
	f := newRefillFixture(t, testSceneSecret, 30)
	refs := f.refs(t, "/many")
	if len(refs) != 30 {
		t.Fatalf("refs = %d", len(refs))
	}
	island.SetSceneEntryStore(island.NewSceneEntryStore(8, 1<<22))
	for i, ref := range refs {
		if w := f.get(ref); w.Code != 200 {
			t.Fatalf("scene %d = %d", i, w.Code)
		}
	}
}

func TestSceneEntryRefillPanicsAreLoggedAndAbortIsRepanicked(t *testing.T) {
	island.SetSceneEntryStore(island.NewSceneEntryStore(8, 1<<20))
	f := newRefillFixture(t, testSceneSecret, 0)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	assertNoStore(t, f.get(signedRef(strings.Repeat("aa", 16), "/boom")), http.StatusNotFound)
	if !strings.Contains(logs.String(), "render exploded") {
		t.Fatalf("panic was swallowed silently: %q", logs.String())
	}
	// The page layer recovers its own panics; a handler that aborts the
	// connection below it must still abort the refill request.
	app := New()
	app.sceneRender = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", v)
		}
	}()
	app.renderSceneRoute(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil), "/abort")
}

func TestSceneEntryErrorsAreNotCacheable(t *testing.T) {
	island.SetSceneEntryStore(island.NewSceneEntryStore(8, 1<<20))
	f := newRefillFixture(t, testSceneSecret, 0)
	for _, ref := range []string{"/gosx/scene3d/short.json", "/gosx/scene3d/" + strings.Repeat("a", 32), island.SceneEntryPath(strings.Repeat("a", 32))} {
		assertNoStore(t, f.get(ref), http.StatusNotFound)
	}
}

// A layout-level public scene has the same hash on every route. Refills for
// two routes that start together both succeed, each from its own render.
func TestSceneEntryTwoRoutesOneHash(t *testing.T) {
	storeA := island.NewSceneEntryStore(8, 1<<20)
	island.SetSceneEntryStore(storeA)
	f := newRefillFixture(t, testSceneSecret, 0)
	refA, refB := f.refs(t, "/a"), f.refs(t, "/b")
	if len(refA) != 1 || len(refB) != 1 || refHash(refA[0]) != refHash(refB[0]) || refA[0] == refB[0] {
		t.Fatalf("setup: same hash, different routes expected: %v %v", refA, refB)
	}
	for i := 0; i < 5; i++ {
		island.SetSceneEntryStore(island.NewSceneEntryStore(8, 1<<20))
		f.renders.Store(0)
		for len(f.entered) > 0 {
			<-f.entered
		}
		f.gate = make(chan struct{})
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for j, ref := range []string{refA[0], refB[0]} {
			wg.Add(1)
			go func(j int, ref string) {
				defer wg.Done()
				codes[j] = f.get(ref).Code
			}(j, ref)
			time.Sleep(10 * time.Millisecond)
		}
		<-f.entered
		<-f.entered
		close(f.gate)
		wg.Wait()
		if codes[0] != 200 || codes[1] != 200 {
			t.Fatalf("run %d: /a=%d /b=%d, want both 200", i, codes[0], codes[1])
		}
		if n := f.renders.Load(); n != 2 {
			t.Fatalf("run %d: renders = %d, want 2 (one per route)", i, n)
		}
	}
}

// Clients that give up quickly still leave a remembered miss behind, so a
// stale signed URL costs one render, not one per request.
func TestSceneEntryStaleURLRemembersTheMissAfterClientsLeave(t *testing.T) {
	island.SetSceneEntryStore(island.NewSceneEntryStore(8, 1<<20))
	f := newRefillFixture(t, testSceneSecret, 0)
	f.gate = make(chan struct{}) // /slow waits until its request ends
	stale := signedRef(strings.Repeat("5a", 16), "/slow")
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, stale, nil).WithContext(ctx))
		cancel()
	}
	if n := f.renders.Load(); n != 1 {
		t.Fatalf("a stale URL replayed by 10 impatient clients rendered %d times, want 1", n)
	}
}

func TestSetSceneEntrySecretIsRefusedAfterBuild(t *testing.T) {
	app := New()
	if err := app.SetSceneEntrySecret(testSceneSecret); err != nil {
		t.Fatal(err)
	}
	app.Build()
	if err := app.SetSceneEntrySecret([]byte("another-secret-0123456789")); err == nil {
		t.Fatal("changing the secret after Build must fail")
	}
	if string(app.sceneEntrySecret()) != string(testSceneSecret) {
		t.Fatal("the secret changed after Build")
	}
}
