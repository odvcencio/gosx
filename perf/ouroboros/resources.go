package ouroboros

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	ResourceManifestSchemaVersion = "gosx.ouroboros.resources.v1"
	CanonicalResourceManifestRef  = "_ouroboros/resources.v1.json"
	MaxResourceManifestBytes      = 4 << 20

	maxResourceManifestResources        = 4096
	maxResourceManifestRoutes           = 12
	maxResourceManifestDynamicEndpoints = 512
	maxResourceManifestExclusions       = 1024
	maxResourceManifestStringBytes      = 1024
)

var resourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type ResourceManifest struct {
	SchemaVersion    string                      `json:"schemaVersion"`
	Contract         string                      `json:"contractVersion"`
	CorpusID         string                      `json:"corpusID"`
	Resources        []ResourceManifestResource  `json:"resources"`
	Routes           []ResourceManifestRoute     `json:"routes"`
	DynamicEndpoints []ResourceManifestDynamic   `json:"dynamicEndpoints"`
	Exclusions       []ResourceManifestExclusion `json:"exclusions,omitempty"`
}

type ResourceManifestResource struct {
	ID           string   `json:"id"`
	URL          string   `json:"url"`
	OutputPath   string   `json:"outputPath"`
	Producer     string   `json:"producer"`
	Kind         string   `json:"kind"`
	Source       string   `json:"source"`
	ContentType  string   `json:"contentType"`
	SHA256       string   `json:"sha256"`
	Bytes        int64    `json:"bytes"`
	GzipBytes    int64    `json:"gzipBytes"`
	BrotliBytes  int64    `json:"brotliBytes"`
	BuildBinding string   `json:"buildBinding,omitempty"`
	Aliases      []string `json:"aliases,omitempty"`
	UsedByRoutes []string `json:"usedByRoutes,omitempty"`
	Parents      []string `json:"parents,omitempty"`
}

type ResourceManifestRoute struct {
	ID        string   `json:"id"`
	Route     string   `json:"route"`
	Resources []string `json:"resources"`
}

type ResourceManifestDynamic struct {
	ID       string `json:"id"`
	RouteID  string `json:"routeID"`
	Route    string `json:"route"`
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	Producer string `json:"producer"`
}

type ResourceManifestExclusion struct {
	ID      string `json:"id"`
	RouteID string `json:"routeID,omitempty"`
	Route   string `json:"route,omitempty"`
	Kind    string `json:"kind"`
	URL     string `json:"url"`
	Reason  string `json:"reason"`
}

type LoadedResourceManifest struct {
	Path      string
	SHA256    string
	Manifest  *ResourceManifest
	Resources []TransferredAsset
	Notes     []string
}

func DecodeResourceManifestStrict(r io.Reader) (*ResourceManifest, error) {
	limited := io.LimitReader(r, MaxResourceManifestBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxResourceManifestBytes {
		return nil, fmt.Errorf("resource manifest exceeds max bytes")
	}
	return decodeResourceManifestBytes(data)
}

func decodeResourceManifestBytes(data []byte) (*ResourceManifest, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var manifest ResourceManifest
	if err := dec.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode resource manifest: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode resource manifest: trailing JSON")
	}
	if err := ValidateResourceManifest(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func LoadResourceManifestStrict(path string) (*ResourceManifest, string, error) {
	lstat, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if lstat.Mode()&os.ModeSymlink != 0 || !lstat.Mode().IsRegular() {
		return nil, "", fmt.Errorf("resource manifest must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	stat, statErr := f.Stat()
	if statErr != nil {
		_ = f.Close()
		return nil, "", statErr
	}
	if !os.SameFile(lstat, stat) {
		_ = f.Close()
		return nil, "", fmt.Errorf("resource manifest changed while opening")
	}
	data, readErr := io.ReadAll(io.LimitReader(f, MaxResourceManifestBytes+1))
	closeErr := f.Close()
	if readErr != nil {
		return nil, "", readErr
	}
	if closeErr != nil {
		return nil, "", closeErr
	}
	if len(data) > MaxResourceManifestBytes {
		return nil, "", fmt.Errorf("resource manifest exceeds max bytes")
	}
	manifest, err := decodeResourceManifestBytes(data)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	return manifest, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func LoadAndValidateResourceManifest(distDir, manifestPath string, canonical bool) (*LoadedResourceManifest, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return nil, nil
	}
	if canonical && filepath.ToSlash(manifestPath) != CanonicalResourceManifestRef {
		return nil, fmt.Errorf("canonical resourceManifest = %q, want %q", manifestPath, CanonicalResourceManifestRef)
	}
	full, err := containedRegularFileNoSymlink(distDir, manifestPath)
	if err != nil {
		return nil, fmt.Errorf("resource manifest path: %w", err)
	}
	manifest, sha, err := LoadResourceManifestStrict(full)
	if err != nil {
		return nil, err
	}
	loaded := &LoadedResourceManifest{Path: full, SHA256: sha, Manifest: manifest}
	assets, err := validateResourceFiles(distDir, manifest, canonical)
	if err != nil {
		return nil, err
	}
	loaded.Resources = assets
	if len(manifest.DynamicEndpoints) > 0 {
		loaded.Notes = append(loaded.Notes, fmt.Sprintf("Resource manifest declares %d dynamic endpoints; they are route-bound and count as zero transferred bytes.", len(manifest.DynamicEndpoints)))
	}
	return loaded, nil
}

func ValidateResourceManifest(manifest *ResourceManifest) error {
	if manifest == nil {
		return fmt.Errorf("missing resource manifest")
	}
	if manifest.SchemaVersion != ResourceManifestSchemaVersion {
		return fmt.Errorf("resource manifest schemaVersion = %q, want %q", manifest.SchemaVersion, ResourceManifestSchemaVersion)
	}
	if manifest.Contract != ContractO02 {
		return fmt.Errorf("resource manifest contractVersion = %q, want %q", manifest.Contract, ContractO02)
	}
	if manifest.CorpusID != CorpusID {
		return fmt.Errorf("resource manifest corpusID = %q, want %q", manifest.CorpusID, CorpusID)
	}
	if len(manifest.Resources) > maxResourceManifestResources ||
		len(manifest.Routes) > maxResourceManifestRoutes ||
		len(manifest.DynamicEndpoints) > maxResourceManifestDynamicEndpoints ||
		len(manifest.Exclusions) > maxResourceManifestExclusions {
		return fmt.Errorf("resource manifest entry bounds exceeded")
	}
	if len(manifest.Routes) != len(canonicalRouteIDs()) {
		return fmt.Errorf("resource manifest route count = %d, want %d", len(manifest.Routes), len(canonicalRouteIDs()))
	}
	resByID := map[string]ResourceManifestResource{}
	seenURLs := map[string]bool{}
	seenPaths := map[string]bool{}
	prevOrderKey := ""
	for _, res := range manifest.Resources {
		if err := validateResourceRowStrings(res); err != nil {
			return err
		}
		if !resourceIDPattern.MatchString(res.ID) {
			return fmt.Errorf("resource manifest invalid resource id %q", res.ID)
		}
		if err := validateResourceURL(res.URL); err != nil {
			return fmt.Errorf("resource %s url: %w", res.ID, err)
		}
		if err := validateResourceOutputPath(res.OutputPath); err != nil {
			return fmt.Errorf("resource %s outputPath: %w", res.ID, err)
		}
		orderKey := res.URL + "\x00" + res.ID
		if prevOrderKey != "" && orderKey <= prevOrderKey {
			return fmt.Errorf("resource manifest resources must be sorted by url then id")
		}
		prevOrderKey = orderKey
		if _, ok := resByID[res.ID]; ok {
			return fmt.Errorf("resource manifest duplicate resource id %s", res.ID)
		}
		if seenURLs[res.URL] || seenPaths[res.OutputPath] {
			return fmt.Errorf("resource manifest duplicate resource URL or outputPath")
		}
		seenURLs[res.URL] = true
		seenPaths[res.OutputPath] = true
		if !validResourceHash(res.SHA256) || res.Bytes < 0 || res.GzipBytes < 0 || res.BrotliBytes < 0 {
			return fmt.Errorf("resource %s has invalid metrics", res.ID)
		}
		if !sortedUniqueNonEmpty(res.Aliases) || !sortedUniqueNonEmpty(res.UsedByRoutes) || !sortedUniqueNonEmpty(res.Parents) {
			return fmt.Errorf("resource %s slices must be sorted unique", res.ID)
		}
		for _, alias := range res.Aliases {
			if err := validateResourceURL(alias); err != nil {
				return fmt.Errorf("resource %s alias: %w", res.ID, err)
			}
		}
		resByID[res.ID] = res
	}
	routeIDs := canonicalRouteIDs()
	routeByID := map[string]ResourceManifestRoute{}
	for i, route := range manifest.Routes {
		if route.ID != routeIDs[i] {
			return fmt.Errorf("resource manifest route[%d] id = %q, want %q", i, route.ID, routeIDs[i])
		}
		if route.Route != canonicalOuroborosRoutePath(route.ID) {
			return fmt.Errorf("resource manifest route %s path = %q", route.ID, route.Route)
		}
		if !sortedUniqueNonEmpty(route.Resources) {
			return fmt.Errorf("resource manifest route %s resources must be sorted unique", route.ID)
		}
		for _, id := range route.Resources {
			if _, ok := resByID[id]; !ok {
				return fmt.Errorf("resource manifest route %s references unknown resource %s", route.ID, id)
			}
		}
		routeByID[route.ID] = route
	}
	if err := validateResourceParents(resByID); err != nil {
		return err
	}
	if err := validateResourceRouteSymmetry(manifest, routeByID); err != nil {
		return err
	}
	resourceIDsAndURLs := map[string]bool{}
	for id := range resByID {
		resourceIDsAndURLs[id] = true
	}
	for url := range seenURLs {
		resourceIDsAndURLs[url] = true
	}
	if err := validateResourceDynamics(manifest.DynamicEndpoints, routeByID, resourceIDsAndURLs); err != nil {
		return err
	}
	for _, exclusion := range manifest.Exclusions {
		if err := validateResourceExclusion(exclusion); err != nil {
			return err
		}
	}
	return nil
}

func validateResourceFiles(distDir string, manifest *ResourceManifest, canonical bool) ([]TransferredAsset, error) {
	ownedRoots := map[string]bool{}
	allowedPaths := map[string]bool{CanonicalResourceManifestRef: true}
	byHash := map[string]string{}
	assets := make([]TransferredAsset, 0, len(manifest.Resources))
	for _, res := range manifest.Resources {
		outputPath := filepath.ToSlash(res.OutputPath)
		allowedPaths[outputPath] = true
		if root := resourceOwnedDir(outputPath); root != "" {
			ownedRoots[root] = true
		}
		metrics, full, err := metricsForContainedRegularFileNoSymlink(distDir, res.OutputPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("resource %s missing file: %w", res.ID, err)
			}
			return nil, fmt.Errorf("resource %s outputPath: %w", res.ID, err)
		}
		if "sha256:"+metrics.SHA256 != res.SHA256 ||
			metrics.Bytes != res.Bytes ||
			metrics.GzipBytes != res.GzipBytes ||
			metrics.BrotliBytes != res.BrotliBytes {
			return nil, fmt.Errorf("resource %s metrics mismatch", res.ID)
		}
		entry := TransferredAsset{
			ID:           res.ID,
			URL:          res.URL,
			Bucket:       res.Kind,
			File:         filepath.Base(full),
			Role:         "resource:" + res.Producer,
			SourcePath:   full,
			ManifestHash: res.SHA256,
			SHA256:       metrics.SHA256,
			Bytes:        metrics.Bytes,
			GzipBytes:    metrics.GzipBytes,
			BrotliBytes:  metrics.BrotliBytes,
			UsedByRoutes: resourceUsedByRoutePaths(manifest, res.ID),
		}
		if prior, ok := byHash[entry.SHA256]; ok {
			entry.DuplicateOf = prior
		} else {
			byHash[entry.SHA256] = entry.ID
		}
		assets = append(assets, entry)
	}
	if canonical {
		if err := rejectExtraResourceFiles(distDir, ownedRoots, allowedPaths, manifest.Exclusions); err != nil {
			return nil, err
		}
	}
	return assets, nil
}

func rejectExtraResourceFiles(distDir string, ownedRoots map[string]bool, allowed map[string]bool, exclusions []ResourceManifestExclusion) error {
	excluded := map[string]bool{}
	for _, exclusion := range exclusions {
		_ = exclusion
	}
	for root := range ownedRoots {
		if root == "" || root == "assets" || root == "static" {
			continue
		}
		fullRoot, err := containedDirNoSymlink(distDir, root)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(fullRoot); os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		err = filepath.WalkDir(fullRoot, func(file string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("resource file tree contains symlink %s", file)
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("resource file tree contains non-regular file %s", file)
			}
			rel, err := filepath.Rel(distDir, file)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if !allowed[rel] && !excluded[rel] {
				return fmt.Errorf("extra resource file under producer path: %s", rel)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func resourceOwnedDir(outputPath string) string {
	clean := filepath.ToSlash(outputPath)
	parts := strings.Split(clean, "/")
	if len(parts) >= 3 && parts[0] == "_ouroboros" {
		return parts[0] + "/" + parts[1]
	}
	dir := path.Dir(clean)
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}

func containedRegularFileNoSymlink(root, rel string) (string, error) {
	full, err := safeJoin(root, rel)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	relBack, err := filepath.Rel(rootAbs, fullAbs)
	if err != nil || relBack == "." || relBack == ".." || strings.HasPrefix(relBack, ".."+string(filepath.Separator)) || filepath.IsAbs(relBack) {
		return "", fmt.Errorf("path escapes root: %s", rel)
	}
	parts := strings.Split(filepath.Clean(relBack), string(filepath.Separator))
	current := rootAbs
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path contains symlink: %s", rel)
		}
		if i == len(parts)-1 {
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("path must be a regular file: %s", rel)
			}
			continue
		}
		if !info.IsDir() {
			return "", fmt.Errorf("path parent is not a directory: %s", rel)
		}
	}
	return fullAbs, nil
}

func metricsForContainedRegularFileNoSymlink(root, rel string) (AssetMetrics, string, error) {
	full, err := containedRegularFileNoSymlink(root, rel)
	if err != nil {
		return AssetMetrics{}, "", err
	}
	lstat, err := os.Lstat(full)
	if err != nil {
		return AssetMetrics{}, "", err
	}
	if lstat.Mode()&os.ModeSymlink != 0 || !lstat.Mode().IsRegular() {
		return AssetMetrics{}, "", fmt.Errorf("path must be a regular file: %s", rel)
	}
	f, err := os.Open(full)
	if err != nil {
		return AssetMetrics{}, "", err
	}
	stat, statErr := f.Stat()
	if statErr != nil {
		_ = f.Close()
		return AssetMetrics{}, "", statErr
	}
	if !os.SameFile(lstat, stat) {
		_ = f.Close()
		return AssetMetrics{}, "", fmt.Errorf("path changed while opening: %s", rel)
	}
	data, readErr := io.ReadAll(f)
	closeErr := f.Close()
	if readErr != nil {
		return AssetMetrics{}, "", readErr
	}
	if closeErr != nil {
		return AssetMetrics{}, "", closeErr
	}
	sum := sha256.Sum256(data)
	return AssetMetrics{
		File:        filepath.Base(full),
		SourcePath:  full,
		SHA256:      hex.EncodeToString(sum[:]),
		Bytes:       int64(len(data)),
		GzipBytes:   GzipLength(data),
		BrotliBytes: BrotliLength(data),
	}, full, nil
}

func containedDirNoSymlink(root, rel string) (string, error) {
	full, err := safeJoin(root, rel)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	relBack, err := filepath.Rel(rootAbs, fullAbs)
	if err != nil || relBack == "." || relBack == ".." || strings.HasPrefix(relBack, ".."+string(filepath.Separator)) || filepath.IsAbs(relBack) {
		return "", fmt.Errorf("path escapes root: %s", rel)
	}
	parts := strings.Split(filepath.Clean(relBack), string(filepath.Separator))
	current := rootAbs
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path contains symlink: %s", rel)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("path must be a directory: %s", rel)
		}
	}
	return fullAbs, nil
}

func resourceUsedByRoutePaths(manifest *ResourceManifest, id string) []string {
	paths := map[string]bool{}
	for _, route := range manifest.Routes {
		ids := resourcesForRouteWithChildren(manifest, route)
		if sort.SearchStrings(ids, id) < len(ids) && ids[sort.SearchStrings(ids, id)] == id {
			paths[route.Route] = true
		}
	}
	out := make([]string, 0, len(paths))
	for route := range paths {
		out = append(out, route)
	}
	sort.Strings(out)
	return out
}

func resourceWithDescendants(manifest *ResourceManifest, id string) map[string]bool {
	out := map[string]bool{id: true}
	changed := true
	for changed {
		changed = false
		for _, res := range manifest.Resources {
			if out[res.ID] {
				continue
			}
			for _, parent := range res.Parents {
				if out[parent] {
					out[res.ID] = true
					changed = true
					break
				}
			}
		}
	}
	return out
}

func resourcesForRouteWithChildren(manifest *ResourceManifest, route ResourceManifestRoute) []string {
	ids := map[string]bool{}
	for _, id := range route.Resources {
		ids[id] = true
		for child := range resourceWithDescendants(manifest, id) {
			ids[child] = true
		}
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func validateResourceRowStrings(res ResourceManifestResource) error {
	for name, value := range map[string]string{
		"id": res.ID, "url": res.URL, "outputPath": res.OutputPath, "producer": res.Producer,
		"kind": res.Kind, "source": res.Source, "contentType": res.ContentType, "sha256": res.SHA256,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("resource %s is empty", name)
		}
		if len(value) > maxResourceManifestStringBytes {
			return fmt.Errorf("resource %s exceeds max length", name)
		}
	}
	return nil
}

func validateResourceURL(raw string) error {
	if strings.TrimSpace(raw) == "" || len(raw) > maxResourceManifestStringBytes {
		return fmt.Errorf("empty or oversized URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "" || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("URL must be root-relative without query or fragment")
	}
	if !strings.HasPrefix(raw, "/") {
		return fmt.Errorf("URL must be root-relative")
	}
	clean := path.Clean(raw)
	if clean != raw || strings.Contains(raw, "//") || strings.Contains(raw, `\`) {
		return fmt.Errorf("URL is not canonical")
	}
	if strings.Contains(raw, "%") {
		return fmt.Errorf("URL must not use percent encoding")
	}
	if strings.Contains(clean, "/../") || strings.HasSuffix(clean, "/..") {
		return fmt.Errorf("URL traverses")
	}
	return nil
}

func validateResourceOutputPath(raw string) error {
	if strings.TrimSpace(raw) == "" || len(raw) > maxResourceManifestStringBytes {
		return fmt.Errorf("empty or oversized path")
	}
	if filepath.IsAbs(raw) || strings.Contains(raw, `\`) {
		return fmt.Errorf("path must be relative slash path")
	}
	clean := path.Clean(raw)
	if clean != raw || clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return fmt.Errorf("path is unsafe")
	}
	return nil
}

func validateResourceParents(resources map[string]ResourceManifestResource) error {
	for id, res := range resources {
		for _, parent := range res.Parents {
			if parent == id {
				return fmt.Errorf("resource %s cannot parent itself", id)
			}
			if _, ok := resources[parent]; !ok {
				return fmt.Errorf("resource %s references unknown parent %s", id, parent)
			}
		}
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("resource parent cycle at %s", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, parent := range resources[id].Parents {
			if err := visit(parent); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for id := range resources {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func validateResourceRouteSymmetry(manifest *ResourceManifest, routes map[string]ResourceManifestRoute) error {
	declared := map[string][]string{}
	for _, route := range manifest.Routes {
		for _, id := range resourcesForRouteWithChildren(manifest, route) {
			declared[id] = append(declared[id], route.ID)
		}
	}
	for _, res := range manifest.Resources {
		if !sameStringSlice(res.UsedByRoutes, declared[res.ID]) {
			return fmt.Errorf("resource %s usedByRoutes mismatch", res.ID)
		}
		for _, routeID := range res.UsedByRoutes {
			if _, ok := routes[routeID]; !ok {
				return fmt.Errorf("resource %s references unknown route %s", res.ID, routeID)
			}
		}
	}
	return nil
}

func validateResourceDynamics(dynamics []ResourceManifestDynamic, routes map[string]ResourceManifestRoute, resources map[string]bool) error {
	prev := ""
	for _, endpoint := range dynamics {
		for name, value := range map[string]string{"id": endpoint.ID, "routeID": endpoint.RouteID, "route": endpoint.Route, "kind": endpoint.Kind, "url": endpoint.URL, "producer": endpoint.Producer} {
			if strings.TrimSpace(value) == "" || len(value) > maxResourceManifestStringBytes {
				return fmt.Errorf("dynamic endpoint %s is empty or oversized", name)
			}
		}
		if endpoint.ID <= prev {
			return fmt.Errorf("dynamic endpoints must be sorted by id")
		}
		prev = endpoint.ID
		route, ok := routes[endpoint.RouteID]
		if !ok || route.Route != endpoint.Route {
			return fmt.Errorf("dynamic endpoint %s route binding mismatch", endpoint.ID)
		}
		if resources[endpoint.ID] || resources[endpoint.URL] {
			return fmt.Errorf("dynamic endpoint %s overlaps resource", endpoint.ID)
		}
		if err := validateResourceURL(endpoint.URL); err != nil {
			return fmt.Errorf("dynamic endpoint %s url: %w", endpoint.ID, err)
		}
		if resourceURLLooksFinite(endpoint.URL) {
			return fmt.Errorf("dynamic endpoint %s URL looks like a finite resource", endpoint.ID)
		}
	}
	return nil
}

func validateResourceExclusion(exclusion ResourceManifestExclusion) error {
	for name, value := range map[string]string{"id": exclusion.ID, "kind": exclusion.Kind, "url": exclusion.URL, "reason": exclusion.Reason} {
		if strings.TrimSpace(value) == "" || len(value) > maxResourceManifestStringBytes {
			return fmt.Errorf("resource exclusion %s is empty or oversized", name)
		}
	}
	if exclusion.RouteID != "" && canonicalOuroborosRoutePath(exclusion.RouteID) != exclusion.Route {
		return fmt.Errorf("resource exclusion %s route binding mismatch", exclusion.ID)
	}
	if err := validateResourceURL(exclusion.URL); err != nil {
		return err
	}
	return nil
}

func validResourceHash(value string) bool {
	if !strings.HasPrefix(value, "sha256:") {
		return false
	}
	hash := strings.TrimPrefix(value, "sha256:")
	if len(hash) != 64 {
		return false
	}
	for _, ch := range hash {
		if !strings.ContainsRune("0123456789abcdef", ch) {
			return false
		}
	}
	return true
}

func resourceURLLooksFinite(raw string) bool {
	clean := path.Clean(raw)
	base := path.Base(clean)
	if base == "." || base == "/" || base == "" {
		return false
	}
	return path.Ext(base) != ""
}

func sortedUniqueNonEmpty(values []string) bool {
	prev := ""
	for _, value := range values {
		if strings.TrimSpace(value) == "" || len(value) > maxResourceManifestStringBytes {
			return false
		}
		if prev != "" && value <= prev {
			return false
		}
		prev = value
	}
	return true
}

func canonicalOuroborosRoutePath(id string) string {
	for path := range canonicalOuroborosRoutePaths() {
		if canonicalOuroborosRouteID(path) == id {
			return path
		}
	}
	return ""
}
