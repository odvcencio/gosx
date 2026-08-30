// Package dev provides the GoSX development proxy and live-reload server.
package dev

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"m31labs.dev/gosx"
)

const (
	defaultPollInterval  = 500 * time.Millisecond
	defaultWatchDebounce = 75 * time.Millisecond
	defaultListenAddr    = ":3000"
)

type snapshotEntry struct {
	ModTime        time.Time
	Size           int64
	Info           os.FileInfo
	ContentHash    [sha256.Size]byte
	HasContentHash bool
}

// dependencyWatchTargets keeps the distinct permissions needed outside the
// recursive project root. dirs allow bounded direct .gsx discovery in an
// imported package root; files allow only an exact canonical discovered
// source whose physical parent may be nested below that root; goFiles is the
// exact active-Go subset selected upstream by the Go tool. Kernel watch
// directories are derived from files, but event and polling filters retain
// the narrower active-input distinction.
type dependencyWatchTargets struct {
	dirs    []string
	files   []string
	goFiles []string
}

type sseEvent struct {
	Name string
	Data string
}

// Server fronts a running GoSX app process during development.
//
// It owns three dev-only concerns:
//   - serves staged runtime assets from BuildDir under stable /gosx/* paths
//   - proxies application traffic to ProxyTarget and injects the reload runtime
//   - watches Dir for source changes, runs PreflightChange for every batch, and
//     triggers OnChange for batches that require a full restart
type Server struct {
	Dir      string
	BuildDir string
	// WatchDirs is an explicit allowlist of package directories outside Dir.
	// Their direct .gsx sources participate in island discovery; Go sources are
	// admitted only through WatchGoFiles. It never broadens the recursive
	// project-root watcher to arbitrary external paths.
	WatchDirs []string
	// WatchFiles is an exact allowlist of canonical discovered sources. It is
	// used only when a source's physical parent differs from its package root;
	// watching that parent does not allow changes to any sibling file.
	WatchFiles []string
	// WatchGoFiles is the exact active-Go subset of WatchFiles, already selected
	// from GoFiles/CgoFiles by the Go tool. Dev hashes and observes only these Go
	// inputs and never reimplements GOOS/GOARCH/cgo/build-tag selection.
	WatchGoFiles []string
	// RefreshWatchDirs recomputes the dependency closure after a handled
	// change, so newly imported packages become watched without restarting the
	// dev proxy. A partial invalid closure is unioned with the last valid
	// allowlist so edits in the new package can recover the quarantined app.
	// It remains as a compatibility seam for directory-only callers.
	RefreshWatchDirs func() ([]string, error)
	// RefreshWatchTargets atomically recomputes package roots, exact canonical
	// source files, and the active-Go subset. New GoSX callers use this seam so
	// build selection and accepted nested physical sources cannot diverge.
	RefreshWatchTargets func() ([]string, []string, []string, error)
	ProxyTarget         string
	PreflightChange     func([]string) error
	OnChange            func() error
	PollInterval        time.Duration
	SceneInspector      bool
	Logf                func(format string, args ...any)

	mu          sync.RWMutex
	clients     map[chan sseEvent]struct{}
	lastBuild   time.Time
	lastError   string
	quarantined bool
	proxyTarget string
	server      *http.Server
	stopWatch   chan struct{}
}

// Handler builds the dev proxy HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /gosx/dev/events", s.handleSSE)
	mux.HandleFunc("GET /gosx/dev/info", s.handleInfo)

	for _, route := range []struct {
		pattern     string
		relative    string
		contentType string
	}{
		{pattern: "GET /gosx/runtime.wasm", relative: "gosx-runtime.wasm", contentType: "application/wasm"},
		{pattern: "GET /gosx/gosx-runtime.wasm", relative: "gosx-runtime.wasm", contentType: "application/wasm"},
		{pattern: "GET /gosx/wasm_exec.js", relative: "wasm_exec.js"},
		{pattern: "GET /gosx/bootstrap.js", relative: "bootstrap.js"},
		{pattern: "GET /gosx/patch.js", relative: "patch.js"},
	} {
		route := route
		mux.HandleFunc(route.pattern, func(w http.ResponseWriter, r *http.Request) {
			s.serveBuildFile(w, r, route.relative, route.contentType)
		})
	}

	mux.Handle("GET /gosx/islands/", http.StripPrefix("/gosx/islands/", s.buildDirFileServer("islands")))
	mux.Handle("GET /gosx/css/", http.StripPrefix("/gosx/css/", s.buildDirFileServer("css")))
	mux.HandleFunc("GET /gosx/assets/", s.serveAssetFile)
	mux.Handle("/", http.HandlerFunc(s.serveProxy))
	return mux
}

// ListenAndServe starts the dev server and its background watcher.
func (s *Server) ListenAndServe(addr string) error {
	if strings.TrimSpace(addr) == "" {
		addr = defaultListenAddr
	}
	s.SetProxyTarget(s.ProxyTarget)

	stopWatch := make(chan struct{})
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	srv.RegisterOnShutdown(func() {
		select {
		case <-stopWatch:
		default:
			close(stopWatch)
		}
	})

	s.mu.Lock()
	s.server = srv
	s.stopWatch = stopWatch
	s.mu.Unlock()

	go s.watchLoop(stopWatch)
	s.logf("listening at http://localhost%s", addr)
	return srv.ListenAndServe()
}

// Shutdown stops the HTTP server and file watcher.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.server
	stopWatch := s.stopWatch
	s.server = nil
	s.stopWatch = nil
	s.mu.Unlock()

	if stopWatch != nil {
		select {
		case <-stopWatch:
		default:
			close(stopWatch)
		}
	}
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// SetProxyTarget updates the current proxied upstream.
func (s *Server) SetProxyTarget(target string) {
	s.mu.Lock()
	s.proxyTarget = strings.TrimSpace(target)
	if s.ProxyTarget == "" {
		s.ProxyTarget = s.proxyTarget
	}
	s.mu.Unlock()
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan sseEvent, 8)
	s.mu.Lock()
	if s.clients == nil {
		s.clients = make(map[chan sseEvent]struct{})
	}
	s.clients[ch] = struct{}{}
	lastBuild := s.lastBuild
	lastError := s.lastError
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	s.writeEvent(w, flusher, sseEvent{
		Name: "connected",
		Data: marshalSSEPayload(map[string]any{
			"version":   gosx.Version,
			"lastBuild": lastBuild.Format(time.RFC3339Nano),
			"error":     lastError,
		}),
	})

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case msg := <-ch:
			s.writeEvent(w, flusher, msg)
		case <-heartbeat.C:
			s.writeEvent(w, flusher, sseEvent{
				Name: "heartbeat",
				Data: marshalSSEPayload(map[string]any{
					"time": time.Now().Format(time.RFC3339Nano),
				}),
			})
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleInfo(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	info := map[string]any{
		"version":     gosx.Version,
		"dir":         s.Dir,
		"buildDir":    s.BuildDir,
		"proxyTarget": s.proxyTarget,
		"lastBuild":   s.lastBuild.Format(time.RFC3339Nano),
		"lastError":   s.lastError,
	}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(info)
}

func (s *Server) serveBuildFile(w http.ResponseWriter, r *http.Request, relative string, contentType string) {
	path := filepath.Join(s.BuildDir, filepath.FromSlash(relative))
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	setDevNoCache(w.Header())
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeFile(w, r, path)
}

func (s *Server) buildDirFileServer(relative string) http.Handler {
	root := s.BuildDir
	if strings.TrimSpace(relative) != "" {
		root = filepath.Join(root, filepath.FromSlash(relative))
	}
	fs := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setDevNoCache(w.Header())
		fs.ServeHTTP(w, r)
	})
}

func (s *Server) serveAssetFile(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/gosx/assets/")
	for _, root := range []string{
		filepath.Join(s.Dir, "dist", "assets"),
		filepath.Join(s.BuildDir, "assets"),
		s.BuildDir,
	} {
		path, ok := safeDevAssetPath(root, rel)
		if !ok {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		setDevNoCache(w.Header())
		http.ServeFile(w, r, path)
		return
	}
	http.NotFound(w, r)
}

func safeDevAssetPath(root, rel string) (string, bool) {
	root = strings.TrimSpace(root)
	rel = filepath.Clean(filepath.FromSlash(strings.TrimSpace(rel)))
	if root == "" || rel == "" || rel == "." {
		return "", false
	}
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	target := filepath.Join(root, rel)
	cleanRoot := filepath.Clean(root)
	cleanTarget := filepath.Clean(target)
	if cleanTarget != cleanRoot && !strings.HasPrefix(cleanTarget, cleanRoot+string(filepath.Separator)) {
		return "", false
	}
	return cleanTarget, true
}

func (s *Server) serveProxy(w http.ResponseWriter, r *http.Request) {
	targetURL, err := s.proxyURL()
	if err != nil {
		http.Error(w, "gosx dev proxy target is not ready", http.StatusBadGateway)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.Host = targetURL.Host
		req.Header.Del("Accept-Encoding")
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if !shouldInjectReloadScript(r, resp) {
			return nil
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()

		injected := s.injectDevScripts(string(body))
		resp.Body = io.NopCloser(strings.NewReader(injected))
		resp.ContentLength = int64(len(injected))
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("ETag")
		resp.Header.Set("Content-Length", strconv.Itoa(len(injected)))
		return nil
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		s.logf("proxy error: %v", err)
		http.Error(rw, "gosx dev: upstream app is unavailable", http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) proxyURL() (*url.URL, error) {
	s.mu.RLock()
	target := strings.TrimSpace(s.proxyTarget)
	s.mu.RUnlock()
	if target == "" {
		return nil, fmt.Errorf("proxy target is empty")
	}
	return url.Parse(target)
}

func shouldInjectReloadScript(req *http.Request, resp *http.Response) bool {
	if req == nil || resp == nil {
		return false
	}
	if req.Method != http.MethodGet {
		return false
	}
	if req.Header.Get("X-GoSX-Navigation") != "" {
		return false
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	return contentType == "" || strings.Contains(contentType, "text/html")
}

func injectReloadScript(body string) string {
	if strings.Contains(body, "data-gosx-dev-reload") {
		return body
	}
	snippet := `<script data-gosx-dev-reload="true">(function(){if(window.__gosxDevReload){return;}window.__gosxDevReload=true;var source=new EventSource("/gosx/dev/events");source.addEventListener("reload",function(){window.location.reload();});source.addEventListener("program",function(event){var payload;try{payload=JSON.parse(event.data||"{}");}catch(_){console.error("[gosx dev] bad program payload");return;}var swap=window.__gosx_reload_program;if(typeof swap!=="function"){window.location.reload();return;}var registry=window.__gosx&&window.__gosx.islands;if(!registry||typeof registry.forEach!=="function"){return;}var fmt=payload.format||"json";var matched=0;registry.forEach(function(entry,islandID){if(!entry||entry.component!==payload.component){return;}matched++;try{var res=swap(islandID,payload.program,fmt);if(typeof res==="string"&&res!==""){console.error("[gosx dev] hot-swap failed for "+islandID+": "+res);}}catch(err){console.error("[gosx dev] hot-swap error for "+islandID+":",err);}});if(matched===0&&console.debug){console.debug("[gosx dev] no live island for component "+payload.component);}});source.addEventListener("patch",function(event){var payload;try{payload=JSON.parse(event.data||"{}");}catch(_){return;}var apply=window.__gosx_apply_patches;if(typeof apply==="function"&&payload.islandID){apply(payload.islandID,payload.patch||"[]");}});source.addEventListener("build-error",function(event){try{var payload=JSON.parse(event.data||"{}");console.error("[gosx dev] build failed:",payload.error||payload);}catch(_){console.error("[gosx dev] build failed");}});source.onerror=function(){console.warn("[gosx dev] reload connection lost");};})();</script>`
	return injectDevScriptSnippet(body, snippet)
}

func (s *Server) injectDevScripts(body string) string {
	body = injectReloadScript(body)
	if !s.SceneInspector || strings.Contains(body, "data-gosx-dev-scene-inspector") {
		return body
	}
	snippet := `<script data-gosx-dev-scene-inspector="true">window.__gosx_scene3d_inspector=true;</script>`
	return injectDevScriptSnippet(body, snippet)
}

func injectDevScriptSnippet(body, snippet string) string {
	if idx := strings.LastIndex(strings.ToLower(body), "</head>"); idx >= 0 {
		return body[:idx] + snippet + "\n" + body[idx:]
	}
	if idx := strings.LastIndex(strings.ToLower(body), "</body>"); idx >= 0 {
		return body[:idx] + snippet + "\n" + body[idx:]
	}
	return body + snippet
}

func (s *Server) watchLoop(stop <-chan struct{}) {
	if strings.TrimSpace(s.Dir) == "" || s.OnChange == nil {
		return
	}
	if err := s.watchWithFSNotify(stop); err == nil {
		return
	} else {
		s.logf("fsnotify watcher unavailable, falling back to polling: %v", err)
	}

	s.watchWithPolling(stop)
}

func (s *Server) watchWithPolling(stop <-chan struct{}) {
	targets, err := normalizeDependencyWatchTargets(s.Dir, s.WatchDirs, s.WatchFiles, s.WatchGoFiles)
	if err != nil {
		s.logf("normalize dependency watch targets: %v", err)
		return
	}
	snapshot, err := watchedSourceSnapshotForTargets(s.Dir, targets)
	if err != nil {
		s.logf("initial snapshot failed: %v", err)
		return
	}

	interval := s.PollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	refreshFailed := false

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			nextTargets, refreshErr := s.refreshDependencyWatchTargetsWithError(targets)
			refreshTransition := (refreshErr != nil) != refreshFailed
			refreshFailed = refreshErr != nil
			targets = nextTargets
			next, err := watchedSourceSnapshotForTargets(s.Dir, targets)
			if err != nil {
				s.logf("snapshot failed: %v", err)
				continue
			}
			changed := changedWatchedPaths(snapshot, next)
			if refreshTransition {
				changed = sortedStringUnion(changed, []string{dependencyRefreshMarker(s.Dir, targets)})
			}
			if len(changed) == 0 {
				continue
			}
			// Acknowledge exactly the filesystem state that produced this
			// callback before invoking a potentially blocking rebuild. A second
			// mutation during OnChange then remains different from this baseline
			// and is delivered on the next poll instead of being absorbed into a
			// post-handler snapshot.
			snapshot = next
			s.handleProjectChange(changed)
		}
	}
}

func (s *Server) watchWithFSNotify(stop <-chan struct{}) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := addProjectWatchDirs(s.Dir, watcher.Add); err != nil {
		return err
	}
	targets, err := normalizeDependencyWatchTargets(s.Dir, s.WatchDirs, s.WatchFiles, s.WatchGoFiles)
	if err != nil {
		return fmt.Errorf("normalize dependency watch targets: %w", err)
	}
	for _, dir := range dependencyKernelWatchDirs(targets) {
		if err := watcher.Add(dir); err != nil {
			return fmt.Errorf("watch dependency directory %s: %w", dir, err)
		}
	}
	dependencySnapshot, err := dependencySourceSnapshotForTargets(targets)
	if err != nil {
		return fmt.Errorf("initial dependency snapshot: %w", err)
	}

	var (
		timer         *time.Timer
		timerC        <-chan time.Time
		pending       = make(map[string]struct{})
		refreshFailed bool
	)
	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer = nil
		timerC = nil
	}
	resetTimer := func() {
		stopTimer()
		timer = time.NewTimer(defaultWatchDebounce)
		timerC = timer.C
	}
	addPending := func(paths []string) bool {
		added := false
		for _, path := range paths {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			path = filepath.Clean(path)
			if _, exists := pending[path]; !exists {
				added = true
			}
			pending[path] = struct{}{}
		}
		return added
	}
	queuePending := func(paths []string) {
		if addPending(paths) {
			resetTimer()
		}
	}
	defer stopTimer()
	repairInterval := s.PollInterval
	if repairInterval <= 0 {
		repairInterval = defaultPollInterval
	}
	repairTicker := time.NewTicker(repairInterval)
	defer repairTicker.Stop()

	for {
		select {
		case <-stop:
			return nil
		case <-repairTicker.C:
			previousRefreshFailed := refreshFailed
			nextTargets, refreshErr := s.reconcileDependencyWatchTargetsWithError(watcher, targets)
			targets = nextTargets
			refreshFailed = refreshErr != nil
			s.repairDependencyTargetWatches(watcher, targets)
			nextSnapshot, snapshotErr := dependencySourceSnapshotForTargets(targets)
			if snapshotErr != nil {
				s.logf("dependency catch-up snapshot failed: %v", snapshotErr)
				continue
			}
			changed := changedWatchedPaths(dependencySnapshot, nextSnapshot)
			if refreshFailed != previousRefreshFailed {
				changed = sortedStringUnion(changed, []string{dependencyRefreshMarker(s.Dir, targets)})
			}
			dependencySnapshot = nextSnapshot
			queuePending(changed)
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			s.logf("watcher error: %v", err)
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Create != 0 {
				s.watchCreatedDirs(event.Name, watcher.Add)
			}
			if isDirectDependencyGoEvent(targets, event) {
				previousRefreshFailed := refreshFailed
				nextTargets, refreshErr := s.reconcileDependencyWatchTargetsWithError(watcher, targets)
				targets = nextTargets
				refreshFailed = refreshErr != nil
				nextSnapshot, snapshotErr := dependencySourceSnapshotForTargets(targets)
				if snapshotErr != nil {
					s.logf("dependency Go-event snapshot failed: %v", snapshotErr)
					queuePending([]string{filepath.Clean(event.Name)})
					continue
				}
				changed := changedWatchedPaths(dependencySnapshot, nextSnapshot)
				if refreshErr != nil {
					// The Go tool could not select the new package state. Deliver the
					// safe direct logical path so strict preflight quarantines it.
					changed = sortedStringUnion(changed, []string{filepath.Clean(event.Name)})
				} else if refreshFailed != previousRefreshFailed {
					changed = sortedStringUnion(changed, []string{dependencyRefreshMarker(s.Dir, targets)})
				}
				dependencySnapshot = nextSnapshot
				queuePending(changed)
				continue
			}
			if !isWatchedSourceEventForTargets(s.Dir, targets, event) {
				continue
			}
			queuePending([]string{canonicalWatchEventPath(event.Name)})
		case <-timerC:
			timer = nil
			timerC = nil
			if len(pending) == 0 {
				continue
			}
			// Refresh and install newly authorized package/physical-parent
			// watches before invoking a potentially blocking rebuild. Compare the
			// bounded dependency snapshot after installation so mutations which
			// landed between the triggering event and the new kernel watch are
			// folded into this batch.
			previousRefreshFailed := refreshFailed
			nextTargets, refreshErr := s.reconcileDependencyWatchTargetsWithError(watcher, targets)
			targets = nextTargets
			refreshFailed = refreshErr != nil
			if refreshFailed != previousRefreshFailed {
				addPending([]string{dependencyRefreshMarker(s.Dir, targets)})
			}
			nextSnapshot, snapshotErr := dependencySourceSnapshotForTargets(targets)
			if snapshotErr != nil {
				s.logf("dependency pre-change catch-up snapshot failed: %v", snapshotErr)
			} else {
				addPending(changedWatchedPaths(dependencySnapshot, nextSnapshot))
				dependencySnapshot = nextSnapshot
			}
			paths := make([]string, 0, len(pending))
			for path := range pending {
				paths = append(paths, path)
			}
			sort.Strings(paths)
			pending = make(map[string]struct{})
			s.handleProjectChange(paths)

			// Hooks or a second filesystem mutation may change the authoritative
			// closure while OnChange is blocked. Reconcile again, then queue a
			// deterministic catch-up batch before returning to the event loop.
			previousRefreshFailed = refreshFailed
			nextTargets, refreshErr = s.reconcileDependencyWatchTargetsWithError(watcher, targets)
			targets = nextTargets
			refreshFailed = refreshErr != nil
			nextSnapshot, snapshotErr = dependencySourceSnapshotForTargets(targets)
			if snapshotErr != nil {
				s.logf("dependency post-change catch-up snapshot failed: %v", snapshotErr)
				continue
			}
			changed := changedWatchedPaths(dependencySnapshot, nextSnapshot)
			if refreshFailed != previousRefreshFailed {
				changed = sortedStringUnion(changed, []string{dependencyRefreshMarker(s.Dir, targets)})
			}
			dependencySnapshot = nextSnapshot
			queuePending(changed)
		}
	}
}

// refreshDependencyWatchTargets asks the caller for the latest validated
// island dependency closure. An unresolved closure preserves the last
// known-good allowlist; a safely resolved but source-invalid partial closure
// is unioned with it so the invalid package and its exact physical sources
// remain observable for recovery.
func (s *Server) refreshDependencyWatchTargets(current dependencyWatchTargets) dependencyWatchTargets {
	next, _ := s.refreshDependencyWatchTargetsWithError(current)
	return next
}

func (s *Server) refreshDependencyWatchTargetsWithError(current dependencyWatchTargets) (dependencyWatchTargets, error) {
	var (
		dirs       []string
		files      []string
		goFiles    []string
		refreshErr error
	)
	switch {
	case s.RefreshWatchTargets != nil:
		dirs, files, goFiles, refreshErr = s.RefreshWatchTargets()
	case s.RefreshWatchDirs != nil:
		dirs, refreshErr = s.RefreshWatchDirs()
		// A directory-only refresh cannot safely recalculate exact source
		// membership, so preserve any explicit initial files.
		files = current.files
		goFiles = current.goFiles
	default:
		return current, nil
	}
	if refreshErr != nil && len(dirs) == 0 && len(files) == 0 && len(goFiles) == 0 {
		s.logf("refresh dependency watch targets failed: %v", refreshErr)
		return current, refreshErr
	}
	// A source-invalid partial closure deliberately omits the unsafe source
	// identity that caused discovery to fail. Normalize its already-canonical
	// roots and exact safe files without re-deriving the same invalid direct
	// symlink; the package root must remain watched so retargeting that logical
	// entry can recover the app. Fully valid refreshes retain strict derivation.
	normalized, normalizeErr := normalizeDependencyWatchTargetsInternal(s.Dir, dirs, files, goFiles, refreshErr == nil)
	if normalizeErr != nil {
		s.logf("normalize refreshed dependency watch targets failed: %v", normalizeErr)
		return current, normalizeErr
	}
	if refreshErr != nil {
		s.logf("refresh dependency watch targets found an invalid closure: %v", refreshErr)
		normalized.dirs = sortedStringUnion(current.dirs, normalized.dirs)
		normalized.files = sortedStringUnion(current.files, normalized.files)
		normalized.goFiles = sortedStringUnion(current.goFiles, normalized.goFiles)
	}
	return normalized, refreshErr
}

// refreshDependencyWatchDirs preserves the directory-only test/API seam.
func (s *Server) refreshDependencyWatchDirs(current []string) []string {
	return s.refreshDependencyWatchTargets(dependencyWatchTargets{dirs: current}).dirs
}

func sortedStringUnion(values ...[]string) []string {
	set := map[string]struct{}{}
	for _, list := range values {
		for _, value := range list {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func dependencyRefreshMarker(root string, targets dependencyWatchTargets) string {
	if len(targets.dirs) > 0 {
		return filepath.Clean(targets.dirs[0])
	}
	return filepath.Clean(root)
}

func dependencyKernelWatchDirs(targets dependencyWatchTargets) []string {
	parents := make([]string, 0, len(targets.files))
	for _, file := range targets.files {
		parents = append(parents, filepath.Clean(filepath.Dir(file)))
	}
	return sortedStringUnion(targets.dirs, parents)
}

func (s *Server) reconcileDependencyWatchTargetsWithError(watcher *fsnotify.Watcher, current dependencyWatchTargets) (dependencyWatchTargets, error) {
	next, refreshErr := s.refreshDependencyWatchTargetsWithError(current)
	s.syncDependencyWatches(watcher, dependencyKernelWatchDirs(current), dependencyKernelWatchDirs(next))
	return next, refreshErr
}

func (s *Server) reconcileDependencyWatchDirs(watcher *fsnotify.Watcher, current []string) []string {
	currentTargets := dependencyWatchTargets{dirs: current}
	return s.reconcileDependencyWatchTargets(watcher, currentTargets).dirs
}

func (s *Server) reconcileDependencyWatchTargets(watcher *fsnotify.Watcher, current dependencyWatchTargets) dependencyWatchTargets {
	next, _ := s.reconcileDependencyWatchTargetsWithError(watcher, current)
	return next
}

// repairDependencyWatches restores kernel watches for logically desired
// dependency directories. Linux drops a directory watch when that directory
// is removed; retaining the logical path and checking WatchList lets a later
// recreation be re-added even though the dependency closure did not change.
func (s *Server) repairDependencyWatches(watcher *fsnotify.Watcher, desired []string) {
	s.syncDependencyWatches(watcher, desired, desired)
}

func (s *Server) repairDependencyTargetWatches(watcher *fsnotify.Watcher, desired dependencyWatchTargets) {
	dirs := dependencyKernelWatchDirs(desired)
	s.syncDependencyWatches(watcher, dirs, dirs)
}

func (s *Server) syncDependencyWatches(watcher *fsnotify.Watcher, current, next []string) {
	liveSet := make(map[string]struct{})
	for _, dir := range watcher.WatchList() {
		liveSet[filepath.Clean(dir)] = struct{}{}
	}
	nextSet := stringSet(next)
	for _, dir := range next {
		dir = filepath.Clean(dir)
		if _, ok := liveSet[dir]; ok {
			continue
		}
		if err := watcher.Add(dir); err != nil {
			s.logf("watch dependency directory %s failed: %v", dir, err)
			continue
		}
		liveSet[dir] = struct{}{}
	}
	for _, dir := range current {
		dir = filepath.Clean(dir)
		if _, ok := nextSet[dir]; ok {
			continue
		}
		if _, ok := liveSet[dir]; !ok {
			continue
		}
		if err := watcher.Remove(dir); err != nil {
			s.logf("unwatch dependency directory %s failed: %v", dir, err)
		}
	}
}

func (s *Server) watchCreatedDirs(path string, add func(string) error) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() || shouldSkipDir(info.Name()) || pathHasSkippedDir(s.Dir, path) {
		return
	}
	if err := addProjectWatchDirs(path, add); err != nil {
		s.logf("watch new directory failed: %v", err)
	}
}

// handleProjectChange routes a batch of changed source paths through the
// dev-socket delivery seam. emitChange classifies the batch: an island-only
// .gsx edit hot-swaps the live island's bytecode (no rebuild, no reload), while
// any server/route/Go/CSS/JS change runs OnChange and triggers a full reload.
func (s *Server) handleProjectChange(paths []string) {
	s.emitChange(paths)
}

func (s *Server) broadcast(name string, payload any) {
	msg := sseEvent{Name: name, Data: marshalSSEPayload(payload)}

	s.mu.RLock()
	clients := make([]chan sseEvent, 0, len(s.clients))
	for ch := range s.clients {
		clients = append(clients, ch)
	}
	s.mu.RUnlock()

	for _, ch := range clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *Server) writeEvent(w http.ResponseWriter, flusher http.Flusher, event sseEvent) {
	if event.Name != "" {
		_, _ = fmt.Fprintf(w, "event: %s\n", event.Name)
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", event.Data)
	flusher.Flush()
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
		return
	}
	log.Printf("[gosx dev] "+format, args...)
}

func marshalSSEPayload(payload any) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return `{"error":"marshal_failure"}`
	}
	return string(data)
}

func addProjectWatchDirs(dir string, add func(string) error) error {
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		if path != dir && shouldSkipDir(entry.Name()) {
			return filepath.SkipDir
		}
		return add(path)
	})
}

func isProjectWatchEvent(root string, event fsnotify.Event) bool {
	if event.Name == "" || !isRelevantWatchOp(event.Op) || pathHasSkippedDir(root, event.Name) {
		return false
	}
	info, err := os.Stat(event.Name)
	if err == nil && info.IsDir() {
		return false
	}
	rel, err := filepath.Rel(root, event.Name)
	if err != nil || relOutsideRoot(rel) {
		return false
	}
	return shouldWatchProjectFile(filepath.ToSlash(rel))
}

func isWatchedSourceEvent(root string, dependencyDirs []string, event fsnotify.Event) bool {
	return isWatchedSourceEventForTargets(root, dependencyWatchTargets{dirs: dependencyDirs}, event)
}

func isWatchedSourceEventForTargets(root string, targets dependencyWatchTargets, event fsnotify.Event) bool {
	if isProjectWatchEvent(root, event) {
		return true
	}
	if event.Name == "" || !isRelevantWatchOp(event.Op) {
		return false
	}
	path := canonicalWatchEventPath(event.Name)
	if path == "" {
		return false
	}
	for _, dir := range targets.dirs {
		if path == filepath.Clean(dir) && event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
			return true
		}
	}
	for _, dir := range dependencyKernelWatchDirs(targets) {
		if path == filepath.Clean(dir) && event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
			return true
		}
	}
	if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
		return false
	}
	// Exact physical identities cover active Go files and accepted nested GSX
	// targets. Same-file comparison keeps an authoritative inode observable
	// through a hardlink without granting permission to unrelated siblings.
	if eventMatchesExactWatchFile(path, event.Name, targets.files) {
		return true
	}
	if !shouldWatchDependencyGSXFile(event.Name) {
		return false
	}

	// Test the logical parent separately from the fully resolved source path.
	// This keeps every direct package-root GSX source observable even when a new
	// symlink is invalid; strict rediscovery can then quarantine it. Go inputs do
	// not inherit this root-wide permission: they must be in the active exact set.
	// Physical edits below a package root still require an exact match above.
	logicalParent := canonicalWatchEventPath(filepath.Dir(event.Name))
	for _, dir := range targets.dirs {
		if logicalParent == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

func isDirectDependencyGoEvent(targets dependencyWatchTargets, event fsnotify.Event) bool {
	if event.Name == "" || !isRelevantWatchOp(event.Op) || strings.ToLower(filepath.Ext(event.Name)) != ".go" {
		return false
	}
	logicalParent := canonicalWatchEventPath(filepath.Dir(event.Name))
	for _, dir := range targets.dirs {
		if logicalParent == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

func eventMatchesExactWatchFile(path, logical string, files []string) bool {
	for _, file := range files {
		if path == filepath.Clean(file) {
			return true
		}
	}
	info, err := os.Stat(logical)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	for _, file := range files {
		candidate, err := os.Stat(file)
		if err == nil && candidate.Mode().IsRegular() && os.SameFile(info, candidate) {
			return true
		}
	}
	return false
}

// normalizeDependencyWatchDirs converts the discovery result into a minimal
// physical allowlist. Project-owned directories are already covered by the
// recursive root watcher, while external package directories are watched only
// at their top level.
func normalizeDependencyWatchDirs(root string, dirs []string) ([]string, error) {
	canonicalRoot, err := canonicalExistingWatchDir(root)
	if err != nil {
		return nil, fmt.Errorf("resolve project watch root: %w", err)
	}
	var (
		out        []string
		identities []os.FileInfo
	)
	for _, candidate := range dirs {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		canonical, err := canonicalExistingWatchDir(candidate)
		if err != nil {
			return nil, fmt.Errorf("resolve dependency watch directory %s: %w", candidate, err)
		}
		if pathWithinWatchRoot(canonical, canonicalRoot) {
			continue
		}
		info, err := os.Stat(canonical)
		if err != nil {
			return nil, err
		}
		duplicate := false
		for _, previous := range identities {
			if os.SameFile(info, previous) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		identities = append(identities, info)
		out = append(out, canonical)
	}
	sort.Strings(out)
	return out, nil
}

func normalizeDependencyWatchTargets(root string, dirs, files, goFiles []string) (dependencyWatchTargets, error) {
	return normalizeDependencyWatchTargetsInternal(root, dirs, files, goFiles, true)
}

func normalizeDependencyWatchTargetsInternal(root string, dirs, files, goFiles []string, deriveDirectSymlinks bool) (dependencyWatchTargets, error) {
	normalizedDirs, err := normalizeDependencyWatchDirs(root, dirs)
	if err != nil {
		return dependencyWatchTargets{}, err
	}
	if deriveDirectSymlinks {
		// Preserve directory-only callers for GSX by deriving exact physical
		// targets from safe direct .gsx symlinks. Active Go files must always be
		// supplied explicitly from GoFiles/CgoFiles; this fallback never guesses
		// build selection and never walks a nested tree.
		derivedFiles, err := directDependencySymlinkTargets(normalizedDirs)
		if err != nil {
			return dependencyWatchTargets{}, err
		}
		files = append(append([]string(nil), files...), derivedFiles...)
	}
	// Every active Go input is also an exact source target. Keep the separately
	// normalized subset for admission while deriving kernel parents from the
	// complete exact-file set.
	files = append(append([]string(nil), files...), goFiles...)
	canonicalRoot, err := canonicalExistingWatchDir(root)
	if err != nil {
		return dependencyWatchTargets{}, fmt.Errorf("resolve project watch root: %w", err)
	}
	normalizedFiles, err := normalizeDependencyWatchFiles(canonicalRoot, normalizedDirs, files)
	if err != nil {
		return dependencyWatchTargets{}, err
	}
	normalizedGoFiles, err := normalizeDependencyWatchFiles(canonicalRoot, normalizedDirs, goFiles)
	if err != nil {
		return dependencyWatchTargets{}, err
	}
	return dependencyWatchTargets{dirs: normalizedDirs, files: normalizedFiles, goFiles: normalizedGoFiles}, nil
}

func normalizeDependencyWatchFiles(canonicalRoot string, normalizedDirs, files []string) ([]string, error) {
	var (
		normalizedFiles []string
		identities      []os.FileInfo
	)
	for _, candidate := range files {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		canonical, info, err := canonicalExistingWatchFile(candidate)
		if err != nil {
			return nil, fmt.Errorf("resolve dependency watch source %s: %w", candidate, err)
		}
		if pathWithinWatchRoot(canonical, canonicalRoot) {
			continue
		}
		contained := false
		for _, dir := range normalizedDirs {
			if pathWithinWatchRoot(canonical, dir) {
				contained = true
				break
			}
		}
		if !contained {
			return nil, fmt.Errorf("dependency watch source %s resolves outside every allowlisted package directory", candidate)
		}
		duplicate := false
		for _, previous := range identities {
			if os.SameFile(info, previous) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		identities = append(identities, info)
		normalizedFiles = append(normalizedFiles, canonical)
	}
	sort.Strings(normalizedFiles)
	return normalizedFiles, nil
}

func directDependencySymlinkTargets(dirs []string) ([]string, error) {
	var targets []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("inspect dependency watch directory %s: %w", dir, err)
		}
		for _, entry := range entries {
			if !shouldWatchDependencyGSXFile(entry.Name()) {
				continue
			}
			entryInfo, err := entry.Info()
			if err != nil {
				return nil, fmt.Errorf("inspect dependency source %s: %w", filepath.Join(dir, entry.Name()), err)
			}
			if entryInfo.Mode()&os.ModeSymlink == 0 {
				continue
			}
			logical := filepath.Join(dir, entry.Name())
			canonical, _, err := canonicalExistingWatchFile(logical)
			if os.IsNotExist(err) {
				// A broken target is represented by the prior refreshed allowlist,
				// if any; there is no new physical identity to authorize here.
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("resolve dependency source %s: %w", logical, err)
			}
			if !pathWithinWatchRoot(canonical, dir) {
				return nil, fmt.Errorf("dependency source %s resolves outside allowlisted package directory %s", logical, dir)
			}
			targets = append(targets, canonical)
		}
	}
	sort.Strings(targets)
	return targets, nil
}

func canonicalExistingWatchDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", path)
	}
	return filepath.Clean(canonical), nil
}

func canonicalExistingWatchFile(path string) (string, os.FileInfo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", nil, err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("%s is not a regular file", path)
	}
	return filepath.Clean(canonical), info, nil
}

func canonicalWatchEventPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	if canonical, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(canonical)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return filepath.Clean(abs)
	}
	return filepath.Join(parent, filepath.Base(abs))
}

func pathWithinWatchRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && !relOutsideRoot(rel)
}

func shouldWatchDependencyGSXFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".gsx")
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func isRelevantWatchOp(op fsnotify.Op) bool {
	return op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0
}

func setDevNoCache(headers http.Header) {
	headers.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	headers.Set("Pragma", "no-cache")
	headers.Set("Expires", "0")
}

func projectSnapshot(dir string) (map[string]snapshotEntry, error) {
	out := make(map[string]snapshotEntry)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() && shouldSkipDir(info.Name()) {
			return filepath.SkipDir
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !shouldWatchProjectFile(rel) {
			return nil
		}
		out[rel] = snapshotEntry{
			ModTime: info.ModTime(),
			Size:    info.Size(),
			Info:    info,
		}
		return nil
	})
	return out, err
}

// watchedSourceSnapshot uses absolute physical paths so project and external
// package changes share one deterministic diff without granting recursive
// access outside the project root.
func watchedSourceSnapshot(root string, dependencyDirs []string) (map[string]snapshotEntry, error) {
	files, err := directDependencySymlinkTargets(dependencyDirs)
	if err != nil {
		return nil, err
	}
	return watchedSourceSnapshotForTargets(root, dependencyWatchTargets{dirs: dependencyDirs, files: files})
}

func watchedSourceSnapshotForTargets(root string, targets dependencyWatchTargets) (map[string]snapshotEntry, error) {
	project, err := projectSnapshot(root)
	if err != nil {
		return nil, err
	}
	out := make(map[string]snapshotEntry, len(project))
	for rel, entry := range project {
		out[filepath.Join(root, filepath.FromSlash(rel))] = entry
	}
	dependency, err := dependencySourceSnapshotForTargets(targets)
	if err != nil {
		return nil, err
	}
	for path, entry := range dependency {
		out[path] = entry
	}
	return out, nil
}

// dependencySourceSnapshotForTargets performs a bounded top-level GSX scan of
// allowlisted package roots, then reads only the exact active Go identities
// selected upstream by the Go tool. A direct GSX symlink may move between
// nested targets inside its canonical package root without first appearing in
// the stale exact-file set. Go sources receive no equivalent root-wide
// permission: GOOS/GOARCH/cgo/build-tag-inactive files never enter this
// snapshot. Periodic content hashing still observes accepted GSX and active Go
// inodes through unenumerated hardlink aliases even when size and mtime are
// restored. No nested sibling is enumerated or admitted.
func dependencySourceSnapshotForTargets(targets dependencyWatchTargets) (map[string]snapshotEntry, error) {
	out := make(map[string]snapshotEntry)
	for _, dir := range targets.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("snapshot dependency directory %s: %w", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !shouldWatchDependencyGSXFile(entry.Name()) {
				continue
			}
			logical := filepath.Join(dir, entry.Name())
			canonical, snapshot, present, err := snapshotDependencySource(logical, dir)
			if err != nil {
				return nil, err
			}
			if present {
				mergeDependencySnapshot(out, canonical, snapshot)
			}
		}
	}
	for _, file := range targets.goFiles {
		dir := containingDependencyWatchDir(file, targets.dirs)
		if dir == "" {
			return nil, fmt.Errorf("active Go dependency source %s is outside every allowlisted package directory", file)
		}
		canonical, snapshot, present, err := snapshotDependencySource(file, dir)
		if err != nil {
			return nil, err
		}
		if present {
			mergeDependencySnapshot(out, canonical, snapshot)
		}
	}
	return out, nil
}

func containingDependencyWatchDir(path string, dirs []string) string {
	path = filepath.Clean(path)
	for _, dir := range dirs {
		if pathWithinWatchRoot(path, filepath.Clean(dir)) {
			return filepath.Clean(dir)
		}
	}
	return ""
}

func snapshotDependencySource(logical, dir string) (string, snapshotEntry, bool, error) {
	canonical := canonicalWatchEventPath(logical)
	if canonical == "" {
		return "", snapshotEntry{}, false, fmt.Errorf("resolve dependency source %s", logical)
	}
	if !pathWithinWatchRoot(canonical, filepath.Clean(dir)) {
		return "", snapshotEntry{}, false, fmt.Errorf("dependency source %s resolves outside allowlisted package directory %s", logical, dir)
	}
	info, err := os.Stat(logical)
	if os.IsNotExist(err) {
		// A removed target is absent from the next snapshot. Polling reports the
		// transition and refresh can later authorize its recreated identity.
		return canonical, snapshotEntry{}, false, nil
	}
	if err != nil {
		return "", snapshotEntry{}, false, fmt.Errorf("snapshot dependency source %s: %w", logical, err)
	}
	if !info.Mode().IsRegular() {
		return canonical, snapshotEntry{}, false, nil
	}
	data, err := os.ReadFile(logical)
	if err != nil {
		return "", snapshotEntry{}, false, fmt.Errorf("read dependency source %s: %w", logical, err)
	}
	return canonical, snapshotEntry{
		ModTime:        info.ModTime(),
		Size:           info.Size(),
		Info:           info,
		ContentHash:    sha256.Sum256(data),
		HasContentHash: true,
	}, true, nil
}

func mergeDependencySnapshot(out map[string]snapshotEntry, path string, next snapshotEntry) {
	if current, ok := out[path]; ok && current.HasContentHash && !next.HasContentHash {
		return
	}
	out[path] = next
}

func changedWatchedPaths(prev map[string]snapshotEntry, next map[string]snapshotEntry) []string {
	changed := make(map[string]struct{})
	for path, prevEntry := range prev {
		nextEntry, ok := next[path]
		if !ok || !sameSnapshotEntry(prevEntry, nextEntry) {
			changed[path] = struct{}{}
		}
	}
	for path := range next {
		if _, ok := prev[path]; !ok {
			changed[path] = struct{}{}
		}
	}
	paths := make([]string, 0, len(changed))
	for path := range changed {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func snapshotChanged(prev map[string]snapshotEntry, next map[string]snapshotEntry) bool {
	if len(prev) != len(next) {
		return true
	}
	for path, prevEntry := range prev {
		nextEntry, ok := next[path]
		if !ok {
			return true
		}
		if !sameSnapshotEntry(prevEntry, nextEntry) {
			return true
		}
	}
	return false
}

// changedPaths diffs two project snapshots and returns the absolute paths of
// added, modified, or removed watched files. The dev-socket delivery seam uses
// these to classify a change (island .gsx hot-swap vs. full reload), so the
// returned paths are absolute to match the fsnotify watcher.
func changedPaths(root string, prev map[string]snapshotEntry, next map[string]snapshotEntry) []string {
	changed := make(map[string]struct{})
	for rel, prevEntry := range prev {
		nextEntry, ok := next[rel]
		if !ok || !sameSnapshotEntry(prevEntry, nextEntry) {
			changed[rel] = struct{}{}
		}
	}
	for rel := range next {
		if _, ok := prev[rel]; !ok {
			changed[rel] = struct{}{}
		}
	}
	paths := make([]string, 0, len(changed))
	for rel := range changed {
		paths = append(paths, filepath.Join(root, filepath.FromSlash(rel)))
	}
	sort.Strings(paths)
	return paths
}

func sameSnapshotEntry(left, right snapshotEntry) bool {
	if !left.ModTime.Equal(right.ModTime) || left.Size != right.Size {
		return false
	}
	if left.Info != nil && right.Info != nil && !os.SameFile(left.Info, right.Info) {
		return false
	}
	if left.HasContentHash != right.HasContentHash || (left.HasContentHash && left.ContentHash != right.ContentHash) {
		return false
	}
	return true
}

func shouldWatchProjectFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".gsx", ".go", ".css", ".js":
		return true
	default:
		return false
	}
}

func pathHasSkippedDir(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || relOutsideRoot(rel) {
		return true
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := 0; i < len(parts)-1; i++ {
		if shouldSkipDir(parts[i]) {
			return true
		}
	}
	return false
}

func relOutsideRoot(rel string) bool {
	rel = filepath.ToSlash(rel)
	return rel == ".." || strings.HasPrefix(rel, "../")
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "build", "dist", "node_modules":
		return true
	default:
		return strings.HasPrefix(name, ".tmp")
	}
}
