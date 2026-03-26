package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const isrBypassHeader = "X-GoSX-ISR-Revalidate"

type isrConfig struct {
	root      string
	staticDir string
	pages     map[string]isrRoute

	mu         sync.Mutex
	state      map[string]isrPageState
	refreshing map[string]bool
}

type isrManifest struct {
	Pages  []string   `json:"pages"`
	Routes []isrRoute `json:"routes,omitempty"`
}

type isrRoute struct {
	Path              string   `json:"path"`
	File              string   `json:"file"`
	RevalidateSeconds int64    `json:"revalidateSeconds,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}

type isrPageState struct {
	GeneratedAt time.Time
	PathVersion uint64
	TagVersions map[string]uint64
}

// EnableISR serves prerendered HTML from static export output and refreshes it
// in the background when a revalidation window or explicit invalidation marks a
// page stale.
func (a *App) EnableISR() {
	if a == nil {
		return
	}
	if a.isr == nil {
		a.isr = &isrConfig{}
	}
}

func (a *App) maybeServeISR(w http.ResponseWriter, r *http.Request, dispatch func(http.ResponseWriter, *http.Request, bool)) bool {
	if a == nil || a.isr == nil || r == nil || dispatch == nil {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if strings.TrimSpace(r.Header.Get(isrBypassHeader)) != "" {
		return false
	}
	if !acceptsHTML(r) {
		return false
	}
	if !a.isr.load(a.effectiveRuntimeRoot()) {
		return false
	}

	page, ok := a.isr.lookup(r.URL.Path)
	if !ok {
		return false
	}

	filePath := filepath.Join(a.isr.staticDir, filepath.FromSlash(page.File))
	info, err := os.Stat(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return false
		}
		if err := a.isr.regenerate(page, a.Revalidator(), dispatch); err != nil {
			return false
		}
		info, err = os.Stat(filePath)
		if err != nil {
			return false
		}
		a.isr.serve(w, r, filePath, info, page, "MISS")
		return true
	}

	stale := a.isr.stale(page, info, a.Revalidator())
	mode := "HIT"
	if stale {
		mode = "STALE"
		a.isr.refresh(page, a.Revalidator(), dispatch)
	}
	a.isr.serve(w, r, filePath, info, page, mode)
	return true
}

func acceptsHTML(r *http.Request) bool {
	if r == nil {
		return false
	}
	accept := strings.TrimSpace(r.Header.Get("Accept"))
	if accept == "" {
		return true
	}
	return strings.Contains(accept, "text/html") || strings.Contains(accept, "*/*")
}

func (c *isrConfig) load(root string) bool {
	if c == nil {
		return false
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}

	bundleRoot, manifestPath, staticDir := resolveISRBundleRoot(root)
	if bundleRoot == "" || manifestPath == "" || staticDir == "" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.root == bundleRoot && c.pages != nil {
		return len(c.pages) > 0
	}

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return false
	}

	var manifest isrManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return false
	}
	if len(manifest.Routes) == 0 {
		manifest.Routes = make([]isrRoute, 0, len(manifest.Pages))
		for _, pagePath := range manifest.Pages {
			manifest.Routes = append(manifest.Routes, isrRoute{
				Path: pagePath,
				File: exportFilePath(pagePath),
			})
		}
	}

	pages := make(map[string]isrRoute, len(manifest.Routes))
	for _, route := range manifest.Routes {
		normalized := normalizeISRPath(route.Path)
		if normalized == "" {
			continue
		}
		if strings.TrimSpace(route.File) == "" {
			route.File = exportFilePath(normalized)
		}
		route.Path = normalized
		route.Tags = compactStrings(route.Tags)
		pages[normalized] = route
	}
	if len(pages) == 0 {
		return false
	}

	c.root = bundleRoot
	c.staticDir = staticDir
	c.pages = pages
	if c.state == nil {
		c.state = make(map[string]isrPageState, len(pages))
	}
	if c.refreshing == nil {
		c.refreshing = make(map[string]bool, len(pages))
	}
	return true
}

func resolveISRBundleRoot(root string) (bundleRoot string, manifestPath string, staticDir string) {
	candidates := []string{
		root,
		filepath.Join(root, "dist"),
	}
	for _, candidate := range candidates {
		manifest := filepath.Join(candidate, "export.json")
		static := filepath.Join(candidate, "static")
		if isFile(manifest) && isDir(static) {
			return candidate, manifest, static
		}
	}
	return "", "", ""
}

func (c *isrConfig) lookup(requestPath string) (isrRoute, bool) {
	if c == nil {
		return isrRoute{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	page, ok := c.pages[normalizeISRPath(requestPath)]
	return page, ok
}

func (c *isrConfig) stale(page isrRoute, info os.FileInfo, revalidator *Revalidator) bool {
	state := c.pageState(page, info)
	if page.RevalidateSeconds > 0 && time.Since(state.GeneratedAt) >= time.Duration(page.RevalidateSeconds)*time.Second {
		return true
	}
	if revalidator != nil && revalidator.pathVersion(page.Path) > state.PathVersion {
		return true
	}
	for _, tag := range page.Tags {
		if revalidator != nil && revalidator.tagVersion(tag) > state.TagVersions[tag] {
			return true
		}
	}
	return false
}

func (c *isrConfig) pageState(page isrRoute, info os.FileInfo) isrPageState {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.state[page.Path]
	if ok {
		if state.TagVersions == nil {
			state.TagVersions = make(map[string]uint64, len(page.Tags))
		}
		return state
	}
	generatedAt := time.Now()
	if info != nil {
		generatedAt = info.ModTime().UTC()
	}
	state = isrPageState{
		GeneratedAt: generatedAt,
		TagVersions: make(map[string]uint64, len(page.Tags)),
	}
	c.state[page.Path] = state
	return state
}

func (c *isrConfig) refresh(page isrRoute, revalidator *Revalidator, dispatch func(http.ResponseWriter, *http.Request, bool)) {
	if c == nil || dispatch == nil {
		return
	}

	c.mu.Lock()
	if c.refreshing[page.Path] {
		c.mu.Unlock()
		return
	}
	c.refreshing[page.Path] = true
	c.mu.Unlock()

	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.refreshing, page.Path)
			c.mu.Unlock()
		}()
		_ = c.regenerate(page, revalidator, dispatch)
	}()
}

func (c *isrConfig) regenerate(page isrRoute, revalidator *Revalidator, dispatch func(http.ResponseWriter, *http.Request, bool)) error {
	if c == nil || dispatch == nil {
		return fmt.Errorf("isr regenerate: dispatch required")
	}

	req := httptest.NewRequest(http.MethodGet, "http://gosx.local"+page.Path, nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set(isrBypassHeader, "1")
	rec := httptest.NewRecorder()
	dispatch(rec, req, true)
	result := rec.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK {
		return fmt.Errorf("isr regenerate %s: unexpected status %d", page.Path, result.StatusCode)
	}

	target := filepath.Join(c.staticDir, filepath.FromSlash(page.File))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	temp := target + ".tmp"
	if err := os.WriteFile(temp, rec.Body.Bytes(), 0644); err != nil {
		return err
	}
	if err := os.Rename(temp, target); err != nil {
		return err
	}

	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	c.updateState(page, info.ModTime(), revalidator)
	return nil
}

func (c *isrConfig) updateState(page isrRoute, generatedAt time.Time, revalidator *Revalidator) {
	if c == nil {
		return
	}
	state := isrPageState{
		GeneratedAt: generatedAt.UTC(),
		TagVersions: make(map[string]uint64, len(page.Tags)),
	}
	if revalidator != nil {
		state.PathVersion = revalidator.pathVersion(page.Path)
		for _, tag := range page.Tags {
			state.TagVersions[tag] = revalidator.tagVersion(tag)
		}
	}
	c.mu.Lock()
	c.state[page.Path] = state
	c.mu.Unlock()
}

func (c *isrConfig) serve(w http.ResponseWriter, r *http.Request, filePath string, info os.FileInfo, page isrRoute, mode string) {
	if w == nil || r == nil {
		return
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	cacheControl := "public, max-age=0, must-revalidate"
	if page.RevalidateSeconds > 0 {
		cacheControl = fmt.Sprintf("public, max-age=0, stale-while-revalidate=%d", page.RevalidateSeconds)
	}
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if mode != "" {
		w.Header().Set("X-GoSX-ISR", mode)
	}
	MarkObservedRequest(r, "isr", page.Path)

	modTime := time.Now()
	if info != nil {
		modTime = info.ModTime()
	}
	http.ServeContent(w, r, filepath.Base(filePath), modTime, bytes.NewReader(data))
}

func normalizeISRPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	if len(value) > 1 && strings.HasSuffix(value, "/") {
		value = strings.TrimRight(value, "/")
	}
	return value
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func exportFilePath(routePath string) string {
	routePath = normalizeISRPath(routePath)
	if routePath == "/" {
		return "index.html"
	}
	clean := strings.Trim(routePath, "/")
	return filepath.Join(filepath.FromSlash(clean), "index.html")
}
