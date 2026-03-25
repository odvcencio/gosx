// Package dev provides a development server for GoSX applications.
//
// Features:
// - File watching with auto-recompile
// - Hot reload via SSE (Server-Sent Events)
// - Static asset serving
// - Hydration manifest injection
// - WASM bundle serving
package dev

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/gosx"
)

// Server is the GoSX development server.
type Server struct {
	// Dir is the project root directory.
	Dir string

	// Addr is the listen address (default ":3000").
	Addr string

	// AppHandler is the main application handler.
	AppHandler http.Handler

	// StaticDir is the path to static assets (default "static").
	StaticDir string

	// BuildDir is the output directory for compiled assets.
	BuildDir string

	mu        sync.Mutex
	clients   map[chan string]bool
	lastBuild time.Time
}

// New creates a development server.
func New(dir string, handler http.Handler) *Server {
	return &Server{
		Dir:        dir,
		Addr:       ":3000",
		AppHandler: handler,
		StaticDir:  filepath.Join(dir, "static"),
		BuildDir:   filepath.Join(dir, "build"),
		clients:    make(map[chan string]bool),
	}
}

// ListenAndServe starts the development server with file watching.
func (s *Server) ListenAndServe() error {
	mux := http.NewServeMux()

	// SSE endpoint for hot reload
	mux.HandleFunc("GET /gosx/dev/events", s.handleSSE)

	// Dev info endpoint
	mux.HandleFunc("GET /gosx/dev/info", s.handleInfo)

	// Static assets
	if info, err := os.Stat(s.StaticDir); err == nil && info.IsDir() {
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(s.StaticDir))))
	}

	// WASM bundles from build dir
	if info, err := os.Stat(s.BuildDir); err == nil && info.IsDir() {
		mux.Handle("/gosx/assets/", http.StripPrefix("/gosx/assets/", http.FileServer(http.Dir(s.BuildDir))))
	}

	// Stable machinery: WASM runtime and wasm_exec.js are large and don't change
	// between hot-reloads. Cache them aggressively.
	cacheServe := func(contentType, path string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			if contentType != "" {
				w.Header().Set("Content-Type", contentType)
			}
			http.ServeFile(w, r, path)
		}
	}
	mux.HandleFunc("GET /gosx/runtime.wasm", cacheServe("application/wasm", filepath.Join(s.BuildDir, "gosx-runtime.wasm")))
	mux.HandleFunc("GET /gosx/wasm_exec.js", cacheServe("", filepath.Join(s.BuildDir, "wasm_exec.js")))

	// Dev assets: JS and island programs change during development. No-cache.
	noCacheServe := func(contentType, path string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
			if contentType != "" {
				w.Header().Set("Content-Type", contentType)
			}
			http.ServeFile(w, r, path)
		}
	}
	mux.HandleFunc("GET /gosx/bootstrap.js", noCacheServe("", filepath.Join(s.Dir, "client", "js", "bootstrap.js")))
	mux.HandleFunc("GET /gosx/patch.js", noCacheServe("", filepath.Join(s.Dir, "client", "js", "patch.js")))

	// Island programs — no-cache (change when components change)
	islandDir := http.Dir(filepath.Join(s.BuildDir, "islands"))
	mux.Handle("GET /gosx/islands/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.StripPrefix("/gosx/islands/", http.FileServer(islandDir)).ServeHTTP(w, r)
	}))

	// Application routes (wrapped with dev injection)
	mux.Handle("/", s.devMiddleware(s.AppHandler))

	// Start file watcher
	go s.watchFiles()

	addr := s.Addr
	log.Printf("[gosx dev] starting at http://localhost%s", addr)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return srv.ListenAndServe()
}

// handleSSE sends server-sent events for hot reload.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan string, 10)
	s.mu.Lock()
	s.clients[ch] = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	// Send initial connected event
	fmt.Fprintf(w, "event: connected\ndata: {\"version\":%q}\n\n", gosx.Version)
	flusher.Flush()

	for {
		select {
		case msg := <-ch:
			fmt.Fprintf(w, "event: reload\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// handleInfo returns dev server information.
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"version":%q,"dir":%q,"lastBuild":%q}`, gosx.Version, s.Dir, s.lastBuild.Format(time.RFC3339))
}

// devMiddleware injects the hot-reload script into HTML responses.
func (s *Server) devMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &responseRecorder{
			ResponseWriter: w,
			body:           &strings.Builder{},
		}
		next.ServeHTTP(rec, r)

		body := rec.body.String()

		if rec.statusCode == 0 {
			rec.statusCode = http.StatusOK
		}

		contentType := rec.Header().Get("Content-Type")
		shouldInject := rec.statusCode < 400 && (contentType == "" || strings.Contains(contentType, "text/html"))

		// Inject dev script before </body> or at the end
		if shouldInject {
			devScript := `<script>
(function(){
  const es = new EventSource("/gosx/dev/events");
  es.addEventListener("reload", function() { location.reload(); });
  es.addEventListener("connected", function(e) {
    console.log("[gosx dev] connected", JSON.parse(e.data));
  });
  es.onerror = function() { setTimeout(function(){ location.reload(); }, 2000); };
})();
</script>`

			if idx := strings.LastIndex(body, "</body>"); idx >= 0 {
				body = body[:idx] + devScript + "\n" + body[idx:]
			} else {
				body += devScript
			}
		}

		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.WriteHeader(rec.statusCode)
		w.Write([]byte(body))
	})
}

// notifyClients sends a reload event to all connected SSE clients.
func (s *Server) notifyClients(reason string) {
	s.mu.Lock()
	for ch := range s.clients {
		select {
		case ch <- fmt.Sprintf(`{"reason":%q,"time":%q}`, reason, time.Now().Format(time.RFC3339)):
		default:
		}
	}
	s.mu.Unlock()
}

// watchFiles polls for file changes in the project directory.
func (s *Server) watchFiles() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	snapshots := make(map[string]time.Time)
	// Initial snapshot
	s.walkFiles(func(path string, info os.FileInfo) {
		snapshots[path] = info.ModTime()
	})

	for range ticker.C {
		changed := false
		s.walkFiles(func(path string, info os.FileInfo) {
			prev, exists := snapshots[path]
			if !exists || info.ModTime().After(prev) {
				changed = true
				snapshots[path] = info.ModTime()
			}
		})

		if changed {
			s.lastBuild = time.Now()
			log.Printf("[gosx dev] change detected, reloading...")
			s.notifyClients("file_change")
		}
	}
}

func (s *Server) walkFiles(fn func(string, os.FileInfo)) {
	filepath.Walk(s.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Skip hidden dirs and build output
		name := info.Name()
		if info.IsDir() && (strings.HasPrefix(name, ".") || name == "build" || name == "node_modules") {
			return filepath.SkipDir
		}
		ext := filepath.Ext(name)
		if ext == ".go" || ext == ".gsx" || ext == ".html" || ext == ".css" || ext == ".js" {
			fn(path, info)
		}
		return nil
	})
}

// responseRecorder captures the response body for injection.
type responseRecorder struct {
	http.ResponseWriter
	body       *strings.Builder
	statusCode int
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.statusCode == 0 {
		r.statusCode = http.StatusOK
	}
	return r.body.Write(b)
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
}
