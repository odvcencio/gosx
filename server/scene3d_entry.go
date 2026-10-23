package server

import (
	"context"
	"log"
	"net/http"
	"os"
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
//
// Only props a scene marked Public are ever stored, so these bytes do not
// depend on the viewer.
func (a *App) serveSceneEntry(w http.ResponseWriter, r *http.Request, file string) {
	hash, ok := strings.CutSuffix(file, ".json")
	store := island.DefaultSceneEntryStore()
	if !ok || store == nil || !island.ValidSceneEntryHash(hash) {
		sceneEntryStatus(w, r, http.StatusNotFound)
		return
	}
	body, ok := store.Get(hash)
	if !ok {
		// Another process, or an earlier run of this one, rendered the page.
		var result sceneRefillResult
		body, result = a.refillSceneEntry(r, store, hash)
		switch result {
		case sceneRefillOK:
		case sceneRefillBusy:
			w.Header().Set("Retry-After", "1")
			sceneEntryStatus(w, r, http.StatusServiceUnavailable)
			return
		case sceneRefillGone:
			return
		default:
			sceneEntryStatus(w, r, http.StatusNotFound)
			return
		}
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

// sceneEntryStatus answers an error without letting a cache keep it: a miss
// today can be a hit after the page renders.
func sceneEntryStatus(w http.ResponseWriter, r *http.Request, status int) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, http.StatusText(status), status)
}

const (
	sceneRefillHeader    = "X-Gosx-Scene-Refill"
	sceneRefillTimeout   = 10 * time.Second
	sceneRefillMaxFlight = 4
	sceneRefillMissTTL   = 30 * time.Second
	sceneRefillMissMax   = 512
	sceneSecretMinLen    = 16
)

type sceneRefillResult int

const (
	sceneRefillMiss sceneRefillResult = iota
	sceneRefillOK
	sceneRefillBusy
	sceneRefillGone
)

// SetSceneEntrySecret sets the key that signs the route inside a deferred
// scene's props URL. With a key, a server that never stored the props can
// render that route again and serve them when the hash matches. Without one
// (and without GOSX_SCENE_ENTRY_SECRET) the URL names no route and a miss is a
// 404. Every process that serves the same site must use the same key.
func (a *App) SetSceneEntrySecret(secret []byte) {
	if len(secret) < sceneSecretMinLen {
		secret = nil
	}
	a.sceneSecret = append([]byte(nil), secret...)
}

func (a *App) sceneEntrySecret() []byte {
	if len(a.sceneSecret) > 0 {
		return a.sceneSecret
	}
	if env := os.Getenv("GOSX_SCENE_ENTRY_SECRET"); len(env) >= sceneSecretMinLen {
		return []byte(env)
	}
	return nil
}

type sceneSecretKey struct{}

func sceneSecretFromContext(ctx context.Context) []byte {
	secret, _ := ctx.Value(sceneSecretKey{}).([]byte)
	return secret
}

// withSceneSecret hands the signing key to pages through the request context.
func (a *App) withSceneSecret(next http.Handler) http.Handler {
	secret := a.sceneEntrySecret()
	if len(secret) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sceneSecretKey{}, secret)))
	})
}

// sceneFlight is one render in progress for a (route, hash) key.
type sceneFlight struct {
	done    chan struct{}
	body    []byte
	found   bool
	aborted bool
}

// sceneRefill bounds the re-renders a props-URL miss can start.
type sceneRefill struct {
	mu     sync.Mutex
	flight map[string]*sceneFlight
	missed map[string]time.Time
	slots  chan struct{}
	// renders counts started renders; tests read it.
	renders int64
}

func newSceneRefill() *sceneRefill {
	return &sceneRefill{
		flight: map[string]*sceneFlight{},
		missed: map[string]time.Time{},
		slots:  make(chan struct{}, sceneRefillMaxFlight),
	}
}

// refillSceneEntry renders the route named by a signed props URL so the page's
// own RenderEngine call stores the props again, and returns the bytes if their
// hash matches. It is bounded: the signature proves this server named the
// route; a render slot is taken without waiting (busy otherwise); one render
// runs per key while later requests wait only as long as their own request
// lives; the render runs as an anonymous visitor and ends when the request
// does; and a miss is remembered for 30 seconds.
func (a *App) refillSceneEntry(r *http.Request, store *island.SceneEntryStore, hash string) ([]byte, sceneRefillResult) {
	route, sig := r.URL.Query().Get("r"), r.URL.Query().Get("s")
	secret := a.sceneEntrySecret()
	if a.sceneRender == nil || r.Header.Get(sceneRefillHeader) != "" || !island.ValidSceneEntryRoute(route) ||
		!island.VerifySceneEntry(secret, route, hash, sig) {
		return nil, sceneRefillMiss
	}
	rf := a.sceneRefillState()
	key := route + "\x00" + hash
	rf.mu.Lock()
	if at, ok := rf.missed[key]; ok && time.Since(at) < sceneRefillMissTTL {
		rf.mu.Unlock()
		return nil, sceneRefillMiss
	}
	if f, ok := rf.flight[key]; ok {
		rf.mu.Unlock()
		select {
		case <-f.done:
		case <-r.Context().Done():
			return nil, sceneRefillGone
		}
		switch {
		case f.found:
			return f.body, sceneRefillOK
		case f.aborted:
			return nil, sceneRefillBusy
		}
		return nil, sceneRefillMiss
	}
	select {
	case rf.slots <- struct{}{}:
	default:
		rf.mu.Unlock()
		return nil, sceneRefillBusy
	}
	f := &sceneFlight{done: make(chan struct{})}
	rf.flight[key] = f
	rf.renders++
	rf.mu.Unlock()

	watch := store.Watch(hash)
	ctx, cancel := context.WithTimeout(r.Context(), sceneRefillTimeout)
	defer func() {
		cancel()
		watch.Close()
		rf.mu.Lock()
		delete(rf.flight, key)
		if !f.found && !f.aborted {
			if len(rf.missed) >= sceneRefillMissMax {
				rf.missed = map[string]time.Time{}
			}
			rf.missed[key] = time.Now()
		}
		rf.mu.Unlock()
		<-rf.slots
		close(f.done)
	}()
	a.renderSceneRoute(ctx, r, route)
	f.body, f.found = watch.Body()
	if f.found {
		store.Put(f.body)
		return f.body, sceneRefillOK
	}
	if ctx.Err() != nil {
		f.aborted = true
		if r.Context().Err() != nil {
			return nil, sceneRefillGone
		}
		return nil, sceneRefillBusy
	}
	return nil, sceneRefillMiss
}

func (a *App) sceneRefillState() *sceneRefill {
	a.sceneRefillOnce.Do(func() { a.sceneRefill = newSceneRefill() })
	return a.sceneRefill
}

type discardResponse struct{ header http.Header }

func (d *discardResponse) Header() http.Header         { return d.header }
func (d *discardResponse) Write(p []byte) (int, error) { return len(p), nil }
func (d *discardResponse) WriteHeader(int)             {}

// renderSceneRoute serves route as an anonymous top-level page navigation and
// discards the response; the render's side effect is the store write.
//
// Identity: the network origin (RemoteAddr, X-Forwarded-*), Host, language and
// user agent are copied so rate limits, locale and host-based routing see the
// visitor. Cookie and Authorization are dropped on purpose, so props that
// depend on a session never reach a public URL. Origin is dropped because a
// page navigation sends none, and Sec-Fetch-* is set to describe a document
// navigation, not the props fetch that triggered it.
func (a *App) renderSceneRoute(ctx context.Context, from *http.Request, route string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, route, nil)
	if err != nil {
		return
	}
	req.Host = from.Host
	req.RemoteAddr = from.RemoteAddr
	for _, name := range []string{"Accept-Language", "User-Agent", "X-Forwarded-For", "X-Forwarded-Proto", "X-Forwarded-Host", "X-Real-Ip"} {
		for _, value := range from.Header.Values(name) {
			req.Header.Add(name, value)
		}
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set(sceneRefillHeader, "1")
	if from.TLS != nil {
		req.TLS = from.TLS
	}
	defer func() {
		if v := recover(); v != nil {
			if v == http.ErrAbortHandler {
				panic(v)
			}
			log.Printf("gosx: scene props refill for %s panicked: %v", route, v)
		}
	}()
	a.sceneRender.ServeHTTP(&discardResponse{header: http.Header{}}, req)
}
