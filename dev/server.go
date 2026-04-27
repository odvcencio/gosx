// Package dev provides the GoSX development proxy and live-reload server.
package dev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/odvcencio/gosx"
)

const (
	defaultPollInterval  = 500 * time.Millisecond
	defaultWatchDebounce = 75 * time.Millisecond
	defaultListenAddr    = ":3000"
)

type snapshotEntry struct {
	ModTime time.Time
	Size    int64
}

type sseEvent struct {
	Name string
	Data string
}

// Server fronts a running GoSX app process during development.
//
// It owns three dev-only concerns:
// - serves staged runtime assets from BuildDir under stable /gosx/* paths
// - proxies application traffic to ProxyTarget and injects the reload runtime
// - watches Dir for source changes and triggers OnChange before notifying clients
type Server struct {
	Dir          string
	BuildDir     string
	ProxyTarget  string
	OnChange     func() error
	PollInterval time.Duration
	Logf         func(format string, args ...any)

	mu          sync.RWMutex
	clients     map[chan sseEvent]struct{}
	lastBuild   time.Time
	lastError   string
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
	mux.Handle("GET /gosx/assets/", http.StripPrefix("/gosx/assets/", s.buildDirFileServer("")))
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

		injected := injectReloadScript(string(body))
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
	snippet := `<script data-gosx-dev-reload="true">(function(){if(window.__gosxDevReload){return;}window.__gosxDevReload=true;var source=new EventSource("/gosx/dev/events");source.addEventListener("reload",function(){window.location.reload();});source.addEventListener("build-error",function(event){try{var payload=JSON.parse(event.data||"{}");console.error("[gosx dev] build failed:",payload.error||payload);}catch(_){console.error("[gosx dev] build failed");}});source.onerror=function(){console.warn("[gosx dev] reload connection lost");};})();</script>`
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
	snapshot, err := projectSnapshot(s.Dir)
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

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			next, err := projectSnapshot(s.Dir)
			if err != nil {
				s.logf("snapshot failed: %v", err)
				continue
			}
			if !snapshotChanged(snapshot, next) {
				continue
			}
			snapshot = next

			s.handleProjectChange("file_change")
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

	var (
		timer   *time.Timer
		timerC  <-chan time.Time
		pending bool
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
	defer stopTimer()

	for {
		select {
		case <-stop:
			return nil
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
			if !isProjectWatchEvent(s.Dir, event) {
				continue
			}
			pending = true
			resetTimer()
		case <-timerC:
			timer = nil
			timerC = nil
			if !pending {
				continue
			}
			pending = false
			s.handleProjectChange("file_change")
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

func (s *Server) handleProjectChange(reason string) {
	if err := s.OnChange(); err != nil {
		s.mu.Lock()
		s.lastError = err.Error()
		s.mu.Unlock()
		s.logf("change handling failed: %v", err)
		s.broadcast("build-error", map[string]any{
			"error": err.Error(),
			"time":  time.Now().Format(time.RFC3339Nano),
		})
		return
	}

	s.mu.Lock()
	s.lastBuild = time.Now()
	s.lastError = ""
	s.mu.Unlock()
	s.logf("change detected, reloading clients")
	s.broadcast("reload", map[string]any{
		"reason": reason,
		"time":   time.Now().Format(time.RFC3339Nano),
	})
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
		}
		return nil
	})
	return out, err
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
		if !prevEntry.ModTime.Equal(nextEntry.ModTime) || prevEntry.Size != nextEntry.Size {
			return true
		}
	}
	return false
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
