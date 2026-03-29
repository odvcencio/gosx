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

	"github.com/odvcencio/gosx/buildmanifest"
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

type isrArtifact struct {
	page     isrRoute
	filePath string
	info     os.FileInfo
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
	a.isr.serve(w, r, artifact.filePath, artifact.info, artifact.page, mode)
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
	info, err := os.Stat(artifact.filePath)
	switch {
	case err == nil:
		artifact.info = info
		return a.isrServeArtifact(artifact, dispatch)
	case !os.IsNotExist(err):
		return isrArtifact{}, "", false
	}
	return a.regenerateISRArtifact(artifact, dispatch)
}

func (a *App) isrServeArtifact(artifact isrArtifact, dispatch func(http.ResponseWriter, *http.Request, bool)) (isrArtifact, string, bool) {
	mode := "HIT"
	if a.isr.stale(artifact.page, artifact.info, a.Revalidator()) {
		mode = "STALE"
		a.isr.refresh(artifact.page, a.Revalidator(), dispatch)
	}
	return artifact, mode, true
}

func (a *App) regenerateISRArtifact(artifact isrArtifact, dispatch func(http.ResponseWriter, *http.Request, bool)) (isrArtifact, string, bool) {
	if err := a.isr.regenerate(artifact.page, a.Revalidator(), dispatch); err != nil {
		return isrArtifact{}, "", false
	}
	info, err := os.Stat(artifact.filePath)
	if err != nil {
		return isrArtifact{}, "", false
	}
	artifact.info = info
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
	c.state = make(map[string]isrPageState, len(pages))
	c.refreshing = make(map[string]bool, len(pages))
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
	staticDir := c.staticDir
	c.mu.Unlock()
	if !ok {
		return isrArtifact{}, false
	}
	filePath, ok := safeArtifactPath(staticDir, page.File)
	if !ok {
		return isrArtifact{}, false
	}
	return isrArtifact{page: page, filePath: filePath}, true
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

	target, ok := safeArtifactPath(c.staticDir, page.File)
	if !ok {
		return fmt.Errorf("isr regenerate %s: invalid file path %q", page.Path, page.File)
	}
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
