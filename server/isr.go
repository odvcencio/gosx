package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/gosx/buildmanifest"
)

const isrBypassHeader = "X-GoSX-ISR-Revalidate"

type isrConfig struct {
	root      string
	staticDir string
	pages     map[string]isrRoute

	mu    sync.Mutex
	store ISRStore
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

type isrArtifact struct {
	page       isrRoute
	bundleRoot string
	staticDir  string
	store      ISRStore
	modTime    time.Time
	body       []byte
}

// EnableISR serves prerendered HTML from static export output and refreshes it
// in the background when a revalidation window or explicit invalidation marks a
// page stale.
func (a *App) EnableISR() {
	if a == nil {
		return
	}
	if a.isr == nil {
		a.isr = &isrConfig{store: a.ISRStore()}
		return
	}
	if a.isr.store == nil {
		a.isr.store = a.ISRStore()
	}
}

func (a *App) maybeServeISR(w http.ResponseWriter, r *http.Request, dispatch func(http.ResponseWriter, *http.Request, bool)) bool {
	if !a.shouldAttemptISR(r, dispatch) {
		return false
	}
	if !a.isr.load(a.effectiveRuntimeRoot()) {
		return false
	}

	artifact, ok := a.isr.artifact(r.URL.Path)
	if !ok {
		return false
	}
	artifact, mode, ok := a.prepareISRArtifact(artifact, dispatch)
	if !ok {
		return false
	}
	a.isr.serve(w, r, artifact, mode)
	return true
}

func (a *App) shouldAttemptISR(r *http.Request, dispatch func(http.ResponseWriter, *http.Request, bool)) bool {
	if a == nil || a.isr == nil || r == nil || dispatch == nil {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if strings.TrimSpace(r.Header.Get(isrBypassHeader)) != "" {
		return false
	}
	return acceptsHTML(r)
}

func (a *App) prepareISRArtifact(artifact isrArtifact, dispatch func(http.ResponseWriter, *http.Request, bool)) (isrArtifact, string, bool) {
	info, err := artifact.store.StatArtifact(artifact.staticDir, artifact.page.Path, artifact.page.File)
	switch {
	case err == nil:
		artifact.modTime = info.ModTime
		return a.isrServeArtifact(artifact, dispatch)
	case !errors.Is(err, ErrISRArtifactNotFound):
		return isrArtifact{}, "", false
	}
	return a.regenerateISRArtifact(artifact, dispatch)
}

func (a *App) isrServeArtifact(artifact isrArtifact, dispatch func(http.ResponseWriter, *http.Request, bool)) (isrArtifact, string, bool) {
	mode := "HIT"
	if a.isr.stale(artifact, a.Revalidator()) {
		stored, err := artifact.store.ReadArtifact(artifact.staticDir, artifact.page.Path, artifact.page.File)
		if err != nil {
			return isrArtifact{}, "", false
		}
		artifact.body = stored.Body
		artifact.modTime = stored.ModTime
		mode = "STALE"
		a.isr.refresh(artifact, a.Revalidator(), dispatch, a.observeOperation)
	}
	return artifact, mode, true
}

func (a *App) regenerateISRArtifact(artifact isrArtifact, dispatch func(http.ResponseWriter, *http.Request, bool)) (isrArtifact, string, bool) {
	started := time.Now()
	info, err := a.isr.regenerate(artifact, a.Revalidator(), dispatch)
	if err != nil {
		a.observeOperation(OperationEvent{
			Component: "isr",
			Operation: "regenerate",
			Target:    artifact.page.Path,
			Status:    "error",
			Duration:  time.Since(started),
			Error:     err.Error(),
		})
		return isrArtifact{}, "", false
	}
	a.observeOperation(OperationEvent{
		Component: "isr",
		Operation: "regenerate",
		Target:    artifact.page.Path,
		Status:    "ok",
		Duration:  time.Since(started),
	})
	artifact.modTime = info.ModTime
	return artifact, "MISS", true
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
	bundleRoot, manifestPath, staticDir, ok := c.resolvedBundle(root)
	if !ok {
		return false
	}
	if c.reuseLoadedBundle(bundleRoot) {
		return true
	}
	pages, ok := loadISRPages(manifestPath)
	if !ok {
		return false
	}
	c.storeBundle(bundleRoot, staticDir, pages)
	return true
}

func (c *isrConfig) resolvedBundle(root string) (bundleRoot string, manifestPath string, staticDir string, ok bool) {
	if c == nil {
		return "", "", "", false
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return "", "", "", false
	}
	bundleRoot, manifestPath, staticDir = resolveISRBundleRoot(root)
	if bundleRoot == "" || manifestPath == "" || staticDir == "" {
		return "", "", "", false
	}
	return bundleRoot, manifestPath, staticDir, true
}

func (c *isrConfig) reuseLoadedBundle(bundleRoot string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.root == bundleRoot && len(c.pages) > 0
}

func loadISRPages(manifestPath string) (map[string]isrRoute, bool) {
	manifest, err := readISRManifest(manifestPath)
	if err != nil {
		return nil, false
	}
	pages := normalizeISRRoutes(routesForISRManifest(manifest))
	return pages, len(pages) > 0
}

func readISRManifest(manifestPath string) (isrManifest, error) {
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return isrManifest{}, err
	}
	var manifest isrManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return isrManifest{}, err
	}
	return manifest, nil
}

func routesForISRManifest(manifest isrManifest) []isrRoute {
	if len(manifest.Routes) > 0 {
		routes := make([]isrRoute, 0, len(manifest.Routes))
		routes = append(routes, manifest.Routes...)
		return routes
	}
	routes := make([]isrRoute, 0, len(manifest.Pages))
	for _, pagePath := range manifest.Pages {
		routes = append(routes, isrRoute{
			Path: pagePath,
			File: buildmanifest.ExportFilePath(pagePath),
		})
	}
	return routes
}

func normalizeISRRoutes(routes []isrRoute) map[string]isrRoute {
	pages := make(map[string]isrRoute, len(routes))
	for _, route := range routes {
		route, ok := normalizeISRRoute(route)
		if !ok {
			continue
		}
		pages[route.Path] = route
	}
	return pages
}

func normalizeISRRoute(route isrRoute) (isrRoute, bool) {
	normalized := normalizeISRPath(route.Path)
	if normalized == "" {
		return isrRoute{}, false
	}
	if strings.TrimSpace(route.File) == "" {
		route.File = buildmanifest.ExportFilePath(normalized)
	}
	route.Path = normalized
	route.Tags = compactStrings(route.Tags)
	return route, true
}

func (c *isrConfig) storeBundle(bundleRoot, staticDir string, pages map[string]isrRoute) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.root = bundleRoot
	c.staticDir = staticDir
	c.pages = pages
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

func (c *isrConfig) artifact(requestPath string) (isrArtifact, bool) {
	if c == nil {
		return isrArtifact{}, false
	}
	normalized := normalizeISRPath(requestPath)
	c.mu.Lock()
	page, ok := c.pages[normalized]
	root := c.root
	staticDir := c.staticDir
	store := c.store
	c.mu.Unlock()
	if !ok {
		return isrArtifact{}, false
	}
	return isrArtifact{page: page, bundleRoot: root, staticDir: staticDir, store: store}, true
}

func (c *isrConfig) stale(artifact isrArtifact, revalidator *Revalidator) bool {
	state := c.pageState(artifact)
	if artifact.page.RevalidateSeconds > 0 && time.Since(state.GeneratedAt) >= time.Duration(artifact.page.RevalidateSeconds)*time.Second {
		return true
	}
	if revalidator != nil && revalidator.pathVersion(artifact.page.Path) > state.PathVersion {
		return true
	}
	for _, tag := range artifact.page.Tags {
		if revalidator != nil && revalidator.tagVersion(tag) > state.TagVersions[tag] {
			return true
		}
	}
	return false
}

func (c *isrConfig) pageState(artifact isrArtifact) ISRPageState {
	if artifact.store == nil {
		return ISRPageState{
			GeneratedAt: artifact.modTime.UTC(),
			TagVersions: map[string]uint64{},
		}
	}
	state, err := artifact.store.LoadState(artifact.bundleRoot, artifact.page.Path, artifact.modTime.UTC())
	if err != nil {
		return ISRPageState{
			GeneratedAt: artifact.modTime.UTC(),
			TagVersions: map[string]uint64{},
		}
	}
	if state.TagVersions == nil {
		state.TagVersions = make(map[string]uint64, len(artifact.page.Tags))
	}
	return state
}

func (c *isrConfig) refresh(artifact isrArtifact, revalidator *Revalidator, dispatch func(http.ResponseWriter, *http.Request, bool), report func(OperationEvent)) {
	if c == nil || dispatch == nil || artifact.store == nil {
		return
	}
	lease, acquired, err := artifact.store.AcquireRefresh(artifact.bundleRoot, artifact.page.Path)
	if err != nil || !acquired {
		return
	}

	go func() {
		started := time.Now()
		defer func() {
			if lease != nil {
				_ = lease.Release()
			}
		}()
		_, err := c.regenerate(artifact, revalidator, dispatch)
		event := OperationEvent{
			Component: "isr",
			Operation: "refresh",
			Target:    artifact.page.Path,
			Duration:  time.Since(started),
		}
		if err != nil {
			event.Status = "error"
			event.Error = err.Error()
			log.Printf("[gosx/isr] refresh %s failed: %v", artifact.page.Path, err)
		} else {
			event.Status = "ok"
		}
		if report != nil {
			report(event)
		}
	}()
}

func (c *isrConfig) regenerate(artifact isrArtifact, revalidator *Revalidator, dispatch func(http.ResponseWriter, *http.Request, bool)) (ISRArtifactInfo, error) {
	if c == nil || dispatch == nil || artifact.store == nil {
		return ISRArtifactInfo{}, fmt.Errorf("isr regenerate: dispatch and store required")
	}

	req := httptest.NewRequest(http.MethodGet, "http://gosx.local"+artifact.page.Path, nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set(isrBypassHeader, "1")
	rec := httptest.NewRecorder()
	dispatch(rec, req, true)
	result := rec.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK {
		return ISRArtifactInfo{}, fmt.Errorf("isr regenerate %s: unexpected status %d", artifact.page.Path, result.StatusCode)
	}

	info, err := artifact.store.WriteArtifact(artifact.staticDir, artifact.page.Path, artifact.page.File, rec.Body.Bytes())
	if err != nil {
		return ISRArtifactInfo{}, err
	}
	c.updateState(artifact, info.ModTime, revalidator)
	return info, nil
}

func (c *isrConfig) updateState(artifact isrArtifact, generatedAt time.Time, revalidator *Revalidator) {
	if c == nil || artifact.store == nil {
		return
	}
	state := ISRPageState{
		GeneratedAt: generatedAt.UTC(),
		TagVersions: make(map[string]uint64, len(artifact.page.Tags)),
	}
	if revalidator != nil {
		state.PathVersion = revalidator.pathVersion(artifact.page.Path)
		for _, tag := range artifact.page.Tags {
			state.TagVersions[tag] = revalidator.tagVersion(tag)
		}
	}
	_ = artifact.store.SaveState(artifact.bundleRoot, artifact.page.Path, state)
}

func (c *isrConfig) serve(w http.ResponseWriter, r *http.Request, artifact isrArtifact, mode string) {
	if w == nil || r == nil {
		return
	}
	data := artifact.body
	if data == nil {
		stored, err := artifact.store.ReadArtifact(artifact.staticDir, artifact.page.Path, artifact.page.File)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		data = stored.Body
		artifact.modTime = stored.ModTime
	}

	cacheControl := "public, max-age=0, must-revalidate"
	if artifact.page.RevalidateSeconds > 0 {
		cacheControl = fmt.Sprintf("public, max-age=0, stale-while-revalidate=%d", artifact.page.RevalidateSeconds)
	}
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if mode != "" {
		w.Header().Set("X-GoSX-ISR", mode)
	}
	MarkObservedRequest(r, "isr", artifact.page.Path)

	modTime := time.Now().UTC()
	if !artifact.modTime.IsZero() {
		modTime = artifact.modTime
	}
	http.ServeContent(w, r, filepath.Base(artifact.page.File), modTime, bytes.NewReader(data))
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

func safeArtifactPath(root, rel string) (string, bool) {
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
