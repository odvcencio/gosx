package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"m31labs.dev/gosx/internal/httpcache"
	"m31labs.dev/gosx/island"
)

// serveSceneEntry serves the props of a deferred Scene3D by content hash.
// The URL is immutable: a different scene has a different hash.
//
// It uses httpcache.MarkImmutableContentAddressedJSON rather than
// MarkImmutableAsset: the latter accepts only script, style, font and image
// content types, and the session writer forces "private" at commit for an
// asset it marked but could not certify (session/session.go commitCookie).
// Without the mark, a global middleware that reads the session would make
// this response private.
func (a *App) serveSceneEntry(w http.ResponseWriter, r *http.Request, file string) {
	hash, ok := strings.CutSuffix(file, ".json")
	store := island.DefaultSceneEntryStore()
	if !ok || store == nil || !island.ValidSceneEntryHash(hash) {
		http.NotFound(w, r)
		return
	}
	body, ok := store.Get(hash)
	if !ok {
		// Another process, or an earlier run of this one, rendered the page.
		// Render its route again to rebuild the props, then look once more.
		a.refillSceneEntry(r, hash)
		body, ok = store.Get(hash)
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("ETag", `"`+hash+`"`)
	httpcache.MarkImmutableContentAddressedJSON(w, r)
	MarkObservedRequest(r, "runtime", island.SceneEntryPath(hash))
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		return
	}
	_, _ = w.Write(body)
}

const (
	sceneRefillHeader    = "X-Gosx-Scene-Refill"
	sceneRefillTimeout   = 10 * time.Second
	sceneRefillMaxFlight = 4
	sceneRefillMissTTL   = 30 * time.Second
	sceneRefillMissMax   = 512
)

// sceneRefill dedupes and bounds the re-renders a props-URL miss can start.
type sceneRefill struct {
	mu     sync.Mutex
	flight map[string]chan struct{}
	missed map[string]time.Time
	slots  chan struct{}
}

func newSceneRefill() *sceneRefill {
	return &sceneRefill{
		flight: map[string]chan struct{}{},
		missed: map[string]time.Time{},
		slots:  make(chan struct{}, sceneRefillMaxFlight),
	}
}

// refillSceneEntry renders the route named by the ?r= parameter of a props URL
// so the page's own RenderEngine call stores the props again. It runs the page
// as the requester (their cookies and language), reaches only app routes, runs
// at most once at a time per route and URL, and remembers a miss for 30 seconds
// so unknown hashes cannot force repeated renders.
func (a *App) refillSceneEntry(r *http.Request, hash string) {
	route := r.URL.Query().Get("r")
	if route == "" || !island.ValidSceneEntryRoute(route) || r.Header.Get(sceneRefillHeader) != "" || a.sceneRender == nil {
		return
	}
	rf := a.sceneRefillState()
	key := route + "\x00" + hash
	rf.mu.Lock()
	if at, ok := rf.missed[key]; ok && time.Since(at) < sceneRefillMissTTL {
		rf.mu.Unlock()
		return
	}
	if wait, ok := rf.flight[key]; ok {
		rf.mu.Unlock()
		<-wait
		return
	}
	done := make(chan struct{})
	rf.flight[key] = done
	rf.mu.Unlock()

	rf.slots <- struct{}{}
	a.renderSceneRoute(r, route)
	<-rf.slots

	rf.mu.Lock()
	delete(rf.flight, key)
	if store := island.DefaultSceneEntryStore(); store != nil {
		if _, ok := store.Get(hash); !ok {
			if len(rf.missed) >= sceneRefillMissMax {
				rf.missed = map[string]time.Time{}
			}
			rf.missed[key] = time.Now()
		}
	}
	rf.mu.Unlock()
	close(done)
}

func (a *App) sceneRefillState() *sceneRefill {
	a.sceneRefillOnce.Do(func() { a.sceneRefill = newSceneRefill() })
	return a.sceneRefill
}

type discardResponse struct{ header http.Header }

func (d *discardResponse) Header() http.Header         { return d.header }
func (d *discardResponse) Write(p []byte) (int, error) { return len(p), nil }
func (d *discardResponse) WriteHeader(int)             {}

func (a *App) renderSceneRoute(from *http.Request, route string) {
	ctx, cancel := context.WithTimeout(context.Background(), sceneRefillTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, route, nil)
	if err != nil {
		return
	}
	req.Host = from.Host
	for _, name := range []string{"Cookie", "Accept-Language", "Authorization", "User-Agent", "X-Forwarded-Proto", "X-Forwarded-Host"} {
		if value := from.Header.Get(name); value != "" {
			req.Header.Set(name, value)
		}
	}
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set(sceneRefillHeader, "1")
	if from.TLS != nil {
		req.TLS = from.TLS
	}
	defer func() { _ = recover() }()
	a.sceneRender.ServeHTTP(&discardResponse{header: http.Header{}}, req)
}
