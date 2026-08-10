package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
	"m31labs.dev/gosx/buildmanifest"
)

const ouroborosExportSchemaVersion = "gosx.ouroboros.export-corpus.v1"

var canonicalOuroborosExportIDs = []string{"R00", "R01", "R02", "R03", "R04", "R05", "R06", "R07", "R08", "R09A", "R09B", "R10"}

type ouroborosCorpusManifest struct {
	SchemaVersion   string                         `json:"schemaVersion"`
	ContractVersion string                         `json:"contractVersion"`
	CorpusID        string                         `json:"corpusID"`
	FixtureApp      string                         `json:"fixtureApp"`
	Authoring       []string                       `json:"authoring"`
	Routes          []ouroborosCorpusManifestRoute `json:"routes"`
}

type ouroborosCorpusManifestRoute struct {
	ID                    string   `json:"id"`
	Route                 string   `json:"route"`
	FixtureApp            string   `json:"fixtureApp"`
	Purpose               string   `json:"purpose,omitempty"`
	ExpectedRuntime       string   `json:"expectedRuntime,omitempty"`
	ExpectedCapabilities  []string `json:"expectedCapabilities"`
	ExpectedTinyGoCurrent string   `json:"expectedTinyGoCurrent"`
	ExpectedTinyGoFuture  string   `json:"expectedTinyGoFuture"`
	ServerBuildMode       string   `json:"serverBuildMode"`
	RequiredInteractions  []string `json:"requiredInteractions"`
	RequiredScreenshots   []string `json:"requiredScreenshots"`
	RoutePlanAssertions   []string `json:"routePlanAssertions"`
	DisallowedAssets      []string `json:"disallowedRuntimeAssets"`
	External              bool     `json:"external,omitempty"`
	Notes                 string   `json:"notes,omitempty"`
}

type ouroborosExportOptions struct {
	RepoRoot   string
	OutDir     string
	CorpusPath string
	FixtureApp string
	DocsApp    string
	Timeout    time.Duration
}

type ouroborosExportBuiltApp struct {
	Name        string
	AppRoot     string
	DistDir     string
	BaseURL     string
	Command     *exec.Cmd
	Manifest    *buildmanifest.Manifest
	stopStarted bool
	stopDone    chan struct{}
}

type ouroborosAssetBinding struct {
	Producer string
	App      *ouroborosExportBuiltApp
	Source   string
	Asset    buildmanifest.HashedAsset
	SHA256   string
}

type ouroborosExportRoute struct {
	ID           string            `json:"id"`
	Path         string            `json:"path"`
	File         string            `json:"file"`
	Producer     string            `json:"producer"`
	Capabilities routeCapabilities `json:"capabilities"`
	SHA256       string            `json:"sha256"`
	Bytes        int64             `json:"bytes"`
	RawSHA256    string            `json:"rawSha256"`
	RawBytes     int64             `json:"rawBytes"`
}

type ouroborosExportAssetRef struct {
	Ref    string `json:"ref"`
	Bucket string `json:"bucket"`
	File   string `json:"file"`
	Hash   string `json:"hash"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type ouroborosExportResourceRef struct {
	ID          string   `json:"id"`
	Ref         string   `json:"ref"`
	File        string   `json:"file"`
	Producer    string   `json:"producer"`
	Kind        string   `json:"kind"`
	Source      string   `json:"source"`
	ContentType string   `json:"contentType"`
	SHA256      string   `json:"sha256"`
	Bytes       int64    `json:"bytes"`
	GzipBytes   int64    `json:"gzipBytes"`
	BrotliBytes int64    `json:"brotliBytes"`
	Routes      []string `json:"routes,omitempty"`
	Parents     []string `json:"parents,omitempty"`
}

type ouroborosExportDynamicRef struct {
	Ref      string   `json:"ref"`
	Producer string   `json:"producer"`
	Kind     string   `json:"kind"`
	Reason   string   `json:"reason"`
	Routes   []string `json:"routes,omitempty"`
}

type ouroborosExportProvenance struct {
	SchemaVersion   string                       `json:"schemaVersion"`
	ContractVersion string                       `json:"contractVersion"`
	CorpusID        string                       `json:"corpusID"`
	GeneratedAt     string                       `json:"generatedAt"`
	RepoRoot        string                       `json:"repoRoot"`
	FixtureApp      string                       `json:"fixtureApp"`
	DocsApp         string                       `json:"docsApp"`
	Routes          []ouroborosExportRoute       `json:"routes"`
	AssetRefs       []ouroborosExportAssetRef    `json:"assetRefs,omitempty"`
	ResourceRefs    []ouroborosExportResourceRef `json:"resourceRefs,omitempty"`
	DynamicRefs     []ouroborosExportDynamicRef  `json:"dynamicRefs,omitempty"`
}

func cmdOuroborosExportCorpus(args []string) {
	fs := flag.NewFlagSet("ouroboros export-corpus", flag.ExitOnError)
	root := fs.String("root", ".", "repository root")
	out := fs.String("out", "", "new canonical corpus dist directory")
	corpus := fs.String("corpus", filepath.Join("examples", "ouroboros-corpus", "fixtures.v1.json"), "O0.2 fixture manifest")
	fixtureApp := fs.String("fixture-app", filepath.Join("examples", "ouroboros-corpus"), "local O0.2 fixture app")
	docsApp := fs.String("docs-app", filepath.Join("examples", "gosx-docs"), "external docs app for R10")
	timeout := fs.Duration("timeout", 30*time.Second, "per-app readiness timeout")
	if err := fs.Parse(args); err != nil {
		fatal("%v", err)
	}
	if fs.NArg() != 0 {
		fatal("gosx ouroboros export-corpus does not take positional arguments")
	}
	if strings.TrimSpace(*out) == "" {
		fatal("gosx ouroboros export-corpus requires --out")
	}
	if err := runOuroborosExportCorpus(ouroborosExportOptions{
		RepoRoot:   *root,
		OutDir:     *out,
		CorpusPath: *corpus,
		FixtureApp: *fixtureApp,
		DocsApp:    *docsApp,
		Timeout:    *timeout,
	}); err != nil {
		fatal("gosx ouroboros export-corpus: %v", err)
	}
}

func ouroborosExportCorpusUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx ouroboros export-corpus - Build canonical O0.2 corpus dist

Usage:
  gosx ouroboros export-corpus --root <repo> --out <dir> --corpus <fixtures.v1.json> --fixture-app <dir> --docs-app <dir> [--timeout 30s]

`)
}

func runOuroborosExportCorpus(opts ouroborosExportOptions) (err error) {
	repoRoot, err := filepath.Abs(defaultString(opts.RepoRoot, "."))
	if err != nil {
		return fmt.Errorf("resolve repo root: %w", err)
	}
	repoRoot = filepath.Clean(repoRoot)
	outDir, err := resolveContainedNewRepoOutputPath(repoRoot, opts.OutDir)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(outDir); err == nil {
		return fmt.Errorf("refusing to overwrite existing --out: %s", outDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect --out: %w", err)
	}
	fixtureApp, err := resolveCanonicalAppPath(repoRoot, opts.FixtureApp, filepath.Join("examples", "ouroboros-corpus"))
	if err != nil {
		return err
	}
	docsApp, err := resolveCanonicalAppPath(repoRoot, opts.DocsApp, filepath.Join("examples", "gosx-docs"))
	if err != nil {
		return err
	}
	corpusPath, err := resolveContainedRepoPath(repoRoot, opts.CorpusPath)
	if err != nil {
		return err
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	routes, corpus, err := loadStrictOuroborosCorpus(corpusPath, fixtureApp, docsApp)
	if err != nil {
		return err
	}

	tempRoot, err := os.MkdirTemp(filepath.Dir(outDir), "."+filepath.Base(outDir)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create sibling temp: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tempRoot)
		}
	}()

	fixture, err := buildAndStartOuroborosExportApp("fixture", fixtureApp, filepath.Join(tempRoot, "fixture-dist"), timeout)
	if err != nil {
		return err
	}
	defer joinOuroborosStopError(&err, fixture, stopOuroborosExportApp)
	docs, err := buildAndStartOuroborosExportApp("docs", docsApp, filepath.Join(tempRoot, "docs-dist"), timeout)
	if err != nil {
		return err
	}
	defer joinOuroborosStopError(&err, docs, stopOuroborosExportApp)

	publishRoot := filepath.Join(tempRoot, "publish")
	publishDist := filepath.Join(publishRoot, "dist")
	if err := os.MkdirAll(filepath.Join(publishDist, "_ouroboros", "raw-html"), 0o755); err != nil {
		return err
	}
	assetRefs := map[string]*ouroborosAssetBinding{}
	resourceRefs := map[string]*ouroborosResourceBinding{}
	dynamicRefs := map[string]*ouroborosDynamicBinding{}
	exportRoutes := make([]strictOuroborosExportRoute, 0, len(routes))
	provenanceRoutes := make([]ouroborosExportRoute, 0, len(routes))
	client := &http.Client{Timeout: timeout}
	for _, route := range routes {
		app := fixture
		producer := "fixture"
		if route.ID == "R10" {
			app = docs
			producer = "docs"
		}
		rawHTML, err := fetchExportPage(client, app.BaseURL+route.Route)
		if err != nil {
			return fmt.Errorf("fetch %s %s: %w", route.ID, route.Route, err)
		}
		if route.ID == "R10" {
			if err := validateOuroborosR10WaterHTML(rawHTML); err != nil {
				return err
			}
		}
		if err := os.WriteFile(filepath.Join(publishDist, "_ouroboros", "raw-html", route.ID+".html"), []byte(rawHTML), 0o644); err != nil {
			return fmt.Errorf("write raw HTML for %s: %w", route.ID, err)
		}
		rawSum := sha256.Sum256([]byte(rawHTML))
		htmlBody := rawHTML
		htmlBody = canonicalizeOuroborosHTMLRefs(htmlBody)
		routeRefs := map[string]struct{}{}
		addExportRuntimeAssetRefs(routeRefs, htmlBody)
		if len(routeRefs) == 0 && route.ID != "R00" {
			return fmt.Errorf("route %s has no /gosx runtime attribution", route.ID)
		}
		for ref := range routeRefs {
			if err := addOuroborosAssetBinding(assetRefs, ref, producer, app); err != nil {
				return err
			}
		}
		if err := collectOuroborosRouteResources(client, app, producer, route.ID, route.Route, htmlBody, publishDist, resourceRefs, dynamicRefs); err != nil {
			return err
		}
		file := buildmanifest.ExportFilePath(route.Route)
		if err := writeExportPage(publishDist, route.Route, htmlBody); err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(htmlBody))
		exportRoutes = append(exportRoutes, strictOuroborosExportRoute{
			Path:         route.Route,
			File:         filepath.ToSlash(file),
			Capabilities: routeCapabilitiesFromHTML(htmlBody),
			SHA256:       "sha256:" + hex.EncodeToString(sum[:]),
			Bytes:        int64(len(htmlBody)),
		})
		provenanceRoutes = append(provenanceRoutes, ouroborosExportRoute{
			ID:           route.ID,
			Path:         route.Route,
			File:         filepath.ToSlash(file),
			Producer:     producer,
			Capabilities: routeCapabilitiesFromHTML(htmlBody),
			SHA256:       "sha256:" + hex.EncodeToString(sum[:]),
			Bytes:        int64(len(htmlBody)),
			RawSHA256:    "sha256:" + hex.EncodeToString(rawSum[:]),
			RawBytes:     int64(len(rawHTML)),
		})
	}
	if err := validateStrictOuroborosExportRoutes(exportRoutes); err != nil {
		return err
	}
	copiedAssets, filtered, err := copyStrictOuroborosAssets(publishDist, assetRefs)
	if err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(publishDist, "build.json"), filtered); err != nil {
		return err
	}
	copiedResources := sortedOuroborosResourceRefs(resourceRefs)
	classifiedDynamicRefs := sortedOuroborosDynamicRefs(dynamicRefs)
	resourceManifest := buildOuroborosResourceManifest(corpus, copiedResources, classifiedDynamicRefs)
	if err := writeJSONFile(filepath.Join(publishDist, CanonicalResourceManifestRef), resourceManifest); err != nil {
		return err
	}
	exportManifest := struct {
		Routes           []strictOuroborosExportRoute `json:"routes"`
		AssetRefs        []string                     `json:"assetRefs,omitempty"`
		ResourceManifest string                       `json:"resourceManifest"`
	}{
		Routes:           exportRoutes,
		AssetRefs:        sortedOuroborosAssetRefs(assetRefs),
		ResourceManifest: CanonicalResourceManifestRef,
	}
	if err := writeJSONFile(filepath.Join(publishDist, "export.json"), exportManifest); err != nil {
		return err
	}
	provenance := ouroborosExportProvenance{
		SchemaVersion:   ouroborosExportSchemaVersion,
		ContractVersion: corpus.ContractVersion,
		CorpusID:        corpus.CorpusID,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		RepoRoot:        repoRoot,
		FixtureApp:      filepath.ToSlash(relOrAbs(repoRoot, fixtureApp)),
		DocsApp:         filepath.ToSlash(relOrAbs(repoRoot, docsApp)),
		Routes:          provenanceRoutes,
		AssetRefs:       copiedAssets,
		ResourceRefs:    copiedResources,
		DynamicRefs:     classifiedDynamicRefs,
	}
	if err := writeJSONFile(filepath.Join(publishDist, "_ouroboros", "export-corpus.json"), provenance); err != nil {
		return err
	}
	if err := validateOuroborosProducerRoot(publishRoot, copiedAssets, provenance); err != nil {
		return err
	}
	if err := stopAndPublishOuroborosExportRoot(fixture, docs, tempRoot, publishRoot, outDir, stopOuroborosExportApp, publishOuroborosExportRoot); err != nil {
		return err
	}
	cleanup = false
	fmt.Fprintf(os.Stderr, "gosx ouroboros export-corpus: wrote %d routes to %s\n", len(exportRoutes), outDir)
	return nil
}

func publishOuroborosExportRoot(tempRoot, publishRoot, outDir string) error {
	if !sameOrContainedPath(publishRoot, tempRoot) || publishRoot == tempRoot {
		return fmt.Errorf("publish root must be inside temp root")
	}
	if err := os.Rename(publishRoot, outDir); err != nil {
		return fmt.Errorf("publish canonical corpus: %w", err)
	}
	if err := os.RemoveAll(tempRoot); err != nil {
		return fmt.Errorf("remove canonical export temp root: %w", err)
	}
	return nil
}

func stopAndPublishOuroborosExportRoot(fixture, docs *ouroborosExportBuiltApp, tempRoot, publishRoot, outDir string, stopFn func(*ouroborosExportBuiltApp) error, publishFn func(string, string, string) error) error {
	if err := stopFn(fixture); err != nil {
		return err
	}
	if err := stopFn(docs); err != nil {
		return err
	}
	return publishFn(tempRoot, publishRoot, outDir)
}

func buildAndStartOuroborosExportApp(name, appRoot, distDir string, timeout time.Duration) (*ouroborosExportBuiltApp, error) {
	sourceSnapshot, err := snapshotOuroborosSourceTree(appRoot)
	if err != nil {
		return nil, fmt.Errorf("snapshot %s source files: %w", name, err)
	}
	if err := validateGeneratedModulesPackageCurrent(appRoot); err != nil {
		return nil, fmt.Errorf("validate %s generated modules package: %w", name, err)
	}
	if err := ensureModuleDependenciesReadonly(appRoot); err != nil {
		return nil, fmt.Errorf("validate %s module dependencies readonly: %w", name, err)
	}
	isolatedAppRoot := filepath.Join(filepath.Dir(distDir), name+"-src")
	if err := copyOuroborosSourceTree(isolatedAppRoot, appRoot); err != nil {
		return nil, fmt.Errorf("copy %s app source for isolated build: %w", name, err)
	}
	if err := RunBuildWithOptions(isolatedAppRoot, BuildOptions{DistDir: distDir, SkipModuleSync: true, ReadonlyModuleDeps: true, SkipBuildHooks: true, SkipStaticPrerender: true}); err != nil {
		return nil, fmt.Errorf("build %s app: %w", name, err)
	}
	if err := sourceSnapshot.Validate(); err != nil {
		return nil, fmt.Errorf("build %s mutated source tree: %w", name, err)
	}
	manifest, err := buildmanifest.Load(filepath.Join(distDir, "build.json"))
	if err != nil {
		return nil, fmt.Errorf("load %s build manifest: %w", name, err)
	}
	port, err := pickFreePort()
	if err != nil {
		return nil, fmt.Errorf("pick %s port: %w", name, err)
	}
	binary := filepath.Join(distDir, "server", "app"+targetExecutableExt())
	cmd := exec.Command(binary)
	cmd.Dir = distDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = childProcessAttributes()
	cmd.Env = append(os.Environ(), "PORT="+port, "GOSX_APP_ROOT="+distDir, "PUBLIC_URL=http://127.0.0.1:"+port)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s app: %w", name, err)
	}
	app := &ouroborosExportBuiltApp{Name: name, AppRoot: appRoot, DistDir: distDir, BaseURL: "http://127.0.0.1:" + port, Command: cmd, Manifest: manifest}
	if err := waitForAppReady(app.BaseURL, timeout); err != nil {
		waitErr := fmt.Errorf("wait for %s app ready: %w", name, err)
		if stopErr := stopOuroborosExportApp(app); stopErr != nil {
			return nil, errors.Join(waitErr, fmt.Errorf("stop %s app after start failure: %w", name, stopErr))
		}
		return nil, waitErr
	}
	return app, nil
}

func joinOuroborosStopError(errp *error, app *ouroborosExportBuiltApp, stop func(*ouroborosExportBuiltApp) error) {
	if app == nil || stop == nil {
		return
	}
	if stopErr := stop(app); stopErr != nil {
		*errp = errors.Join(*errp, fmt.Errorf("stop %s app: %w", app.Name, stopErr))
	}
}

func copyOuroborosSourceTree(dstRoot, srcRoot string) error {
	srcRoot, err := filepath.Abs(srcRoot)
	if err != nil {
		return err
	}
	dstRoot, err = filepath.Abs(dstRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dstRoot, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(srcRoot, func(src string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if src == srcRoot {
			return nil
		}
		if entry.IsDir() && ouroborosSourceSnapshotSkipDir(entry.Name()) {
			return filepath.SkipDir
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, src)
		if err != nil {
			return err
		}
		dst, err := containedOuroborosOutputPath(dstRoot, rel)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("canonical export refuses source symlink: %s", src)
		}
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported source file type: %s", src)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode().Perm())
	})
}

type ouroborosSourceSnapshot struct {
	appRoot string
	files   map[string]ouroborosSourceSnapshotFile
}

type ouroborosSourceSnapshotFile struct {
	path    string
	exists  bool
	content []byte
}

func snapshotOuroborosSourceTree(appRoot string) (ouroborosSourceSnapshot, error) {
	appRoot, err := filepath.Abs(appRoot)
	if err != nil {
		return ouroborosSourceSnapshot{}, err
	}
	appRoot = filepath.Clean(appRoot)
	out := ouroborosSourceSnapshot{appRoot: appRoot, files: map[string]ouroborosSourceSnapshotFile{}}
	moduleRoot, _, err := moduleInfo(appRoot)
	if err != nil {
		return out, err
	}
	for _, file := range []string{filepath.Join(moduleRoot, "go.mod"), filepath.Join(moduleRoot, "go.sum")} {
		if err := out.addFile("module:"+file, file); err != nil {
			return out, err
		}
	}
	err = filepath.WalkDir(appRoot, func(p string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == appRoot {
			return nil
		}
		if entry.IsDir() && ouroborosSourceSnapshotSkipDir(entry.Name()) {
			return filepath.SkipDir
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(appRoot, p)
			if err != nil {
				return err
			}
			out.files["app:"+filepath.ToSlash(rel)] = ouroborosSourceSnapshotFile{path: p, exists: true, content: []byte("symlink:" + target)}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported source file type: %s", p)
		}
		rel, err := filepath.Rel(appRoot, p)
		if err != nil {
			return err
		}
		return out.addFile("app:"+filepath.ToSlash(rel), p)
	})
	return out, err
}

func ouroborosSourceSnapshotSkipDir(name string) bool {
	switch name {
	case ".git", ".gosx", "build", "dist", "node_modules":
		return true
	default:
		return false
	}
}

func (s ouroborosSourceSnapshot) addFile(key, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.files[key] = ouroborosSourceSnapshotFile{path: path}
			return nil
		}
		return err
	}
	s.files[key] = ouroborosSourceSnapshotFile{path: path, exists: true, content: data}
	return nil
}

func (s ouroborosSourceSnapshot) Validate() error {
	after, err := snapshotOuroborosSourceTree(s.appRoot)
	if err != nil {
		return err
	}
	for key, before := range s.files {
		next, ok := after.files[key]
		if !ok {
			return fmt.Errorf("%s disappeared from source snapshot", before.path)
		}
		if before.exists != next.exists {
			return fmt.Errorf("%s existence changed", before.path)
		}
		if !bytes.Equal(before.content, next.content) {
			return fmt.Errorf("%s changed", before.path)
		}
	}
	for key, next := range after.files {
		if _, ok := s.files[key]; !ok && next.exists {
			return fmt.Errorf("%s was added to source tree", next.path)
		}
	}
	return nil
}

func validateGeneratedModulesPackageCurrent(appRoot string) error {
	moduleRoot, modulePath, err := moduleInfo(appRoot)
	if err != nil {
		return err
	}
	imports, err := discoverModuleImports(appRoot, moduleRoot, modulePath)
	if err != nil {
		return err
	}
	path := filepath.Join(appRoot, "modules", "modules.go")
	got, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	want := []byte(renderModulesPackage(imports))
	if !bytes.Equal(got, want) && !bytes.Equal(bytes.TrimRight(got, " \t\r\n"), bytes.TrimRight(want, " \t\r\n")) {
		return fmt.Errorf("%s is stale; run gosx build outside canonical capture", path)
	}
	return nil
}

func stopOuroborosExportApp(app *ouroborosExportBuiltApp) error {
	return stopOuroborosExportAppWithTimeout(app, 3*time.Second, interruptProcessTree, terminateProcessTree)
}

func stopOuroborosExportAppWithTimeout(app *ouroborosExportBuiltApp, timeout time.Duration, interruptFn, terminateFn func(int) error) error {
	if app == nil || app.Command == nil || app.Command.Process == nil {
		return nil
	}
	done := app.ensureStopWait()
	_ = interruptFn(app.Command.Process.Pid)
	select {
	case <-done:
		app.Command = nil
		return nil
	case <-time.After(timeout):
	}
	_ = terminateFn(app.Command.Process.Pid)
	select {
	case <-done:
		app.Command = nil
		return nil
	case <-time.After(timeout):
	}
	return fmt.Errorf("process %s did not exit after bounded stop", app.Name)
}

func (app *ouroborosExportBuiltApp) ensureStopWait() chan struct{} {
	if app.stopDone == nil {
		app.stopDone = make(chan struct{})
	}
	if app.stopStarted {
		return app.stopDone
	}
	app.stopStarted = true
	cmd := app.Command
	go func() {
		_ = cmd.Wait()
		close(app.stopDone)
	}()
	return app.stopDone
}

func loadStrictOuroborosCorpus(path, fixtureApp, docsApp string) ([]ouroborosCorpusManifestRoute, ouroborosCorpusManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ouroborosCorpusManifest{}, fmt.Errorf("read corpus manifest: %w", err)
	}
	var corpus ouroborosCorpusManifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&corpus); err != nil {
		return nil, ouroborosCorpusManifest{}, fmt.Errorf("decode corpus manifest: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, ouroborosCorpusManifest{}, fmt.Errorf("decode corpus manifest: trailing JSON")
	}
	if corpus.SchemaVersion != "gosx.ouroboros.fixtures.v1" || corpus.ContractVersion != "O0.2" {
		return nil, corpus, fmt.Errorf("unexpected corpus contract %s/%s", corpus.SchemaVersion, corpus.ContractVersion)
	}
	if len(corpus.Routes) != len(canonicalOuroborosExportIDs) {
		return nil, corpus, fmt.Errorf("corpus must contain exact 12 routes, got %d", len(corpus.Routes))
	}
	wantPaths := map[string]string{
		"R00": "/static", "R01": "/lite", "R02": "/island/counter", "R03": "/islands/kitchen",
		"R04": "/action/form", "R05": "/canvas-board", "R06": "/hub/echo", "R07": "/video-sync",
		"R08": "/scene/basic", "R09A": "/navigation/a", "R09B": "/navigation/b", "R10": "/demos/water",
	}
	seen := map[string]bool{}
	for i, id := range canonicalOuroborosExportIDs {
		route := corpus.Routes[i]
		if route.ID != id {
			return nil, corpus, fmt.Errorf("route %d id = %q, want %q", i, route.ID, id)
		}
		if seen[route.ID] {
			return nil, corpus, fmt.Errorf("duplicate route id %s", route.ID)
		}
		seen[route.ID] = true
		if route.Route != wantPaths[id] {
			return nil, corpus, fmt.Errorf("route %s path = %q, want %q", id, route.Route, wantPaths[id])
		}
		if id == "R10" {
			if !route.External {
				return nil, corpus, fmt.Errorf("R10 must be external")
			}
			if !sameCleanSlashPath(route.FixtureApp, "examples/gosx-docs") {
				return nil, corpus, fmt.Errorf("R10 must come from examples/gosx-docs, got %s", route.FixtureApp)
			}
			continue
		}
		if route.External {
			return nil, corpus, fmt.Errorf("%s must not be external", id)
		}
		if !sameCleanSlashPath(route.FixtureApp, "examples/ouroboros-corpus") {
			return nil, corpus, fmt.Errorf("%s must come from examples/ouroboros-corpus, got %s", id, route.FixtureApp)
		}
	}
	return corpus.Routes, corpus, nil
}

func canonicalizeOuroborosHTMLRefs(input string) string {
	replacer := strings.NewReplacer(
		`"/assets/`, `"/gosx/assets/`,
		`'/assets/`, `'/gosx/assets/`,
		`(/assets/`, `(/gosx/assets/`,
		`&quot;/assets/`, `&quot;/gosx/assets/`,
	)
	return replacer.Replace(input)
}

func validateOuroborosR10WaterHTML(input string) error {
	if strings.Contains(input, "data-fixture-local-copy") {
		return fmt.Errorf("R10 response contains fixture-local copy marker")
	}
	for _, marker := range []string{
		`data-demo-slug="water"`,
		`class="water-demo`,
		`Flagship GoSX Scene3D water`,
	} {
		if !strings.Contains(input, marker) {
			return fmt.Errorf("R10 response missing external water marker %q", marker)
		}
	}
	return nil
}

type ouroborosResourceBinding struct {
	ID          string
	Ref         string
	File        string
	Producer    string
	Kind        string
	Source      string
	ContentType string
	SHA256      string
	Bytes       int64
	GzipBytes   int64
	BrotliBytes int64
	Routes      map[string]bool
	Parents     map[string]bool
}

type ouroborosDynamicBinding struct {
	Ref      string
	Producer string
	Kind     string
	Reason   string
	Routes   map[string]bool
}

const (
	ouroborosMaxResourceBytes      = int64(64 << 20)
	ouroborosMaxTotalResourceBytes = int64(192 << 20)
	ouroborosMaxResources          = 256
	ouroborosMaxResourceDepth      = 4
)

var localResourceURLPattern = regexp.MustCompile(`(?:"|'|\(|\s|^)((?:https?://[^"'<>\s)]+|/[^"'<>\s)]+|\./[^"'<>\s)]+|\.\./[^"'<>\s)]+))`)

func collectOuroborosRouteResources(client *http.Client, app *ouroborosExportBuiltApp, producer, routeID, routePath, htmlBody, outputDir string, refs map[string]*ouroborosResourceBinding, dynamics map[string]*ouroborosDynamicBinding) error {
	queue := []ouroborosResourceCandidate{}
	candidates, err := discoverOuroborosResourceCandidates(htmlBody, htmlResourceBaseDir(routePath))
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if candidate.Dynamic {
			if err := addOuroborosDynamicRef(dynamics, candidate.Ref, producer, routeID, candidate.Source, candidate.Reason); err != nil {
				return err
			}
			continue
		}
		queue = append(queue, candidate)
	}
	seenThisRoute := map[string]bool{}
	for len(queue) > 0 {
		candidate := queue[0]
		queue = queue[1:]
		if seenThisRoute[candidate.Ref] {
			if candidate.Parent != "" {
				if binding := refs[candidate.Ref]; binding != nil {
					binding.Parents[canonicalOuroborosResourceID(candidate.Parent)] = true
				}
			}
			continue
		}
		seenThisRoute[candidate.Ref] = true
		nested, err := fetchAndBindOuroborosResource(client, app, producer, routeID, outputDir, refs, candidate)
		if err != nil {
			return err
		}
		queue = append(queue, nested...)
	}
	return nil
}

func htmlResourceBaseDir(routePath string) string {
	clean := path.Clean("/" + strings.TrimLeft(routePath, "/"))
	if clean == "/" {
		return "/"
	}
	if strings.HasSuffix(routePath, "/") {
		return clean
	}
	return path.Dir(clean)
}

type ouroborosResourceCandidate struct {
	Ref     string
	BaseDir string
	Source  string
	Parent  string
	Depth   int
	Dynamic bool
	Reason  string
}

func discoverOuroborosResourceCandidates(input, baseDir string) ([]ouroborosResourceCandidate, error) {
	found := map[string]ouroborosResourceCandidate{}
	var firstErr error
	var addWithSource func(string, string)
	add := func(raw string) {
		addWithSource(raw, "html")
	}
	addWithSource = func(raw, source string) {
		candidate, ok, err := classifyOuroborosResourceURL(raw, baseDir, source, "", 0)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return
		}
		if ok {
			found[candidate.Ref] = candidate
		}
	}
	doc, err := html.Parse(strings.NewReader(input))
	if err == nil {
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
				for _, attr := range n.Attr {
					key := strings.ToLower(attr.Key)
					if isOuroborosTransferAttr(key) {
						addWithSource(attr.Val, key)
					}
					if key == "srcset" {
						for _, value := range splitOuroborosSrcset(attr.Val) {
							add(value)
						}
					}
					if key == "style" {
						for _, value := range discoverOuroborosCSSResourceURLs(attr.Val) {
							addWithSource(value, "css")
						}
					}
					if strings.HasPrefix(key, "data-gosx") {
						for _, value := range discoverTypedOuroborosJSONResourceURLs(attr.Val) {
							addWithSource(value.URL, value.Source)
						}
					}
				}
			}
			if n.Type == html.TextNode && strings.TrimSpace(n.Data) != "" && isOuroborosTransferTextNode(n) {
				if n.Parent != nil && strings.EqualFold(n.Parent.Data, "style") {
					for _, value := range discoverOuroborosCSSResourceURLs(n.Data) {
						addWithSource(value, "css")
					}
				} else {
					for _, value := range discoverTypedOuroborosJSONResourceURLs(n.Data) {
						addWithSource(value.URL, value.Source)
					}
				}
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(doc)
	} else {
		for _, match := range localResourceURLPattern.FindAllStringSubmatch(input, -1) {
			add(match[1])
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	out := make([]ouroborosResourceCandidate, 0, len(found))
	for _, candidate := range found {
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out, nil
}

type typedOuroborosResourceURL struct {
	URL    string
	Source string
}

func discoverTypedOuroborosJSONResourceURLs(input string) []typedOuroborosResourceURL {
	var doc any
	if err := json.Unmarshal([]byte(input), &doc); err != nil {
		return nil
	}
	out := []typedOuroborosResourceURL{}
	collectTypedOuroborosJSONResourceURLs(doc, "", &out)
	collectOuroborosManifestHubPaths(doc, &out)
	return out
}

func collectTypedOuroborosJSONResourceURLs(value any, key string, out *[]typedOuroborosResourceURL) {
	switch v := value.(type) {
	case map[string]any:
		for k, nested := range v {
			collectTypedOuroborosJSONResourceURLs(nested, strings.ToLower(k), out)
		}
	case []any:
		for _, nested := range v {
			collectTypedOuroborosJSONResourceURLs(nested, key, out)
		}
	case string:
		source, ok := typedOuroborosResourceSourceForJSONKey(key)
		if ok {
			*out = append(*out, typedOuroborosResourceURL{URL: v, Source: source})
		}
	}
}

func collectOuroborosManifestHubPaths(value any, out *[]typedOuroborosResourceURL) {
	m, ok := value.(map[string]any)
	if !ok {
		if values, ok := value.([]any); ok {
			for _, nested := range values {
				collectOuroborosManifestHubPaths(nested, out)
			}
		}
		return
	}
	if hubs, ok := m["hubs"].([]any); ok {
		for _, item := range hubs {
			hub, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if pathValue, ok := hub["path"].(string); ok {
				*out = append(*out, typedOuroborosResourceURL{URL: pathValue, Source: "hub"})
			}
		}
	}
	for _, nested := range m {
		switch nested.(type) {
		case map[string]any, []any:
			collectOuroborosManifestHubPaths(nested, out)
		}
	}
}

func typedOuroborosResourceSourceForJSONKey(key string) (string, bool) {
	switch key {
	case "programref", "src", "href", "poster", "modeltexture", "tiletexture", "url", "uri":
		return "json", true
	case "sync":
		return "sync", true
	case "hub", "huburl", "hubendpoint":
		return "hub", true
	case "action", "actionurl", "endpoint":
		return "action", true
	default:
		return "", false
	}
}

func discoverOuroborosCSSResourceURLs(input string) []string {
	out := []string{}
	for _, match := range regexp.MustCompile(`url\(([^)]+)\)`).FindAllStringSubmatch(input, -1) {
		out = append(out, strings.TrimSpace(strings.Trim(match[1], `"'`)))
	}
	for _, match := range regexp.MustCompile(`@import\s+(?:url\()?["']?([^"')\s;]+)`).FindAllStringSubmatch(input, -1) {
		out = append(out, strings.TrimSpace(match[1]))
	}
	return out
}

func isOuroborosTransferAttr(key string) bool {
	switch key {
	case "src", "href", "poster", "action", "formaction":
		return true
	default:
		return false
	}
}

func splitOuroborosSrcset(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) > 0 {
			out = append(out, fields[0])
		}
	}
	return out
}

func isOuroborosTransferTextNode(n *html.Node) bool {
	if n == nil || n.Parent == nil || n.Parent.Type != html.ElementNode {
		return false
	}
	switch strings.ToLower(n.Parent.Data) {
	case "style":
		return true
	case "script":
		for _, attr := range n.Parent.Attr {
			key := strings.ToLower(attr.Key)
			val := strings.ToLower(attr.Val)
			if key == "type" && (strings.Contains(val, "json") || strings.Contains(val, "importmap")) {
				return true
			}
			if key == "id" && val == "gosx-document" {
				return true
			}
		}
	}
	return false
}

func classifyOuroborosResourceURL(raw, baseDir, source, parent string, depth int) (ouroborosResourceCandidate, bool, error) {
	raw = strings.TrimSpace(strings.Trim(raw, `"'`))
	if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "data:") || strings.HasPrefix(raw, "blob:") || strings.HasPrefix(raw, "javascript:") {
		return ouroborosResourceCandidate{}, false, nil
	}
	parsed, err := neturl.Parse(raw)
	if err != nil {
		return ouroborosResourceCandidate{}, false, fmt.Errorf("parse resource URL %q: %w", raw, err)
	}
	if source == "href" && parsed.Scheme == "" && parsed.Host == "" && parsed.Path == "" && parsed.RawPath == "" && (parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "") {
		return ouroborosResourceCandidate{}, false, nil
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return ouroborosResourceCandidate{}, false, fmt.Errorf("resource URL %q must not contain query or fragment", raw)
	}
	if parsed.RawPath != "" || strings.Contains(parsed.EscapedPath(), "%") {
		return ouroborosResourceCandidate{}, false, fmt.Errorf("resource URL %q must not contain percent-encoded path bytes", raw)
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		if isTypedOuroborosDynamicSource(source) {
			return ouroborosResourceCandidate{}, false, fmt.Errorf("external dynamic endpoint is not allowed: %s", raw)
		}
		if isFiniteOuroborosResourcePath(parsed.Path) {
			return ouroborosResourceCandidate{}, false, fmt.Errorf("external resource URL is not allowed: %s", raw)
		}
		return ouroborosResourceCandidate{}, false, nil
	}
	refPath := parsed.Path
	if refPath == "" {
		return ouroborosResourceCandidate{}, false, nil
	}
	if !strings.HasPrefix(refPath, "/") {
		refPath = path.Join(baseDir, refPath)
	}
	refPath = path.Clean("/" + strings.TrimLeft(refPath, "/"))
	if strings.HasPrefix(refPath, "/gosx/") {
		return ouroborosResourceCandidate{}, false, nil
	}
	if isFiniteOuroborosResourcePath(refPath) {
		return ouroborosResourceCandidate{Ref: refPath, BaseDir: path.Dir(refPath), Source: source, Parent: parent, Depth: depth}, true, nil
	}
	if strings.HasPrefix(refPath, "/_ouroboros/") || isTypedOuroborosDynamicSource(source) {
		return ouroborosResourceCandidate{Ref: refPath, Source: source, Parent: parent, Dynamic: true, Reason: "dynamic local endpoint"}, true, nil
	}
	return ouroborosResourceCandidate{}, false, nil
}

func isTypedOuroborosDynamicSource(source string) bool {
	switch source {
	case "action", "formaction", "sync", "hub":
		return true
	default:
		return false
	}
}

func isFiniteOuroborosResourcePath(ref string) bool {
	switch strings.ToLower(path.Ext(ref)) {
	case ".json", ".mp4", ".gltf", ".glb", ".bin", ".png", ".jpg", ".jpeg", ".webp", ".css", ".js", ".wasm":
		return true
	default:
		return false
	}
}

func addOuroborosDynamicRef(dynamics map[string]*ouroborosDynamicBinding, ref, producer, routeID, kind, reason string) error {
	if ref == "" {
		return nil
	}
	kind = defaultString(kind, "endpoint")
	binding := dynamics[ref]
	if binding == nil {
		binding = &ouroborosDynamicBinding{Ref: ref, Producer: producer, Kind: kind, Reason: reason, Routes: map[string]bool{}}
		dynamics[ref] = binding
	} else {
		if binding.Producer != producer {
			return fmt.Errorf("dynamic endpoint %s produced by both %s and %s", ref, binding.Producer, producer)
		}
		if binding.Kind != kind {
			return fmt.Errorf("dynamic endpoint %s has conflicting kinds %s and %s", ref, binding.Kind, kind)
		}
	}
	binding.Routes[routeID] = true
	return nil
}

func fetchAndBindOuroborosResource(client *http.Client, app *ouroborosExportBuiltApp, producer, routeID, outputDir string, refs map[string]*ouroborosResourceBinding, candidate ouroborosResourceCandidate) ([]ouroborosResourceCandidate, error) {
	if candidate.Depth > ouroborosMaxResourceDepth {
		return nil, fmt.Errorf("resource %s exceeds discovery depth", candidate.Ref)
	}
	resourceClient := *client
	resourceClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	req, err := http.NewRequest(http.MethodGet, app.BaseURL+candidate.Ref, nil)
	if err != nil {
		return nil, err
	}
	resp, err := resourceClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch resource %s: %w", candidate.Ref, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, fmt.Errorf("resource %s redirected with status %d", candidate.Ref, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("resource %s returned status %d", candidate.Ref, resp.StatusCode)
	}
	data, err := readBoundedOuroborosResource(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read resource %s: %w", candidate.Ref, err)
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	if strings.Contains(strings.ToLower(contentType), "text/html") && strings.ToLower(path.Ext(candidate.Ref)) != ".html" {
		return nil, fmt.Errorf("resource %s returned HTML content type %s", candidate.Ref, contentType)
	}
	sum := sha256.Sum256(data)
	sha := "sha256:" + hex.EncodeToString(sum[:])
	outputRel, err := localResourceOutputRel(candidate.Ref)
	if err != nil {
		return nil, err
	}
	outputPath := filepath.Join(outputDir, filepath.FromSlash(outputRel))
	if existing := refs[candidate.Ref]; existing != nil {
		if existing.Producer != producer {
			return nil, fmt.Errorf("resource %s is produced by both %s and %s", candidate.Ref, existing.Producer, producer)
		}
		if existing.SHA256 != sha || existing.Bytes != int64(len(data)) || existing.ContentType != contentType {
			return nil, fmt.Errorf("resource %s has conflicting duplicate content", candidate.Ref)
		}
		existing.Routes[routeID] = true
		if candidate.Parent != "" {
			existing.Parents[canonicalOuroborosResourceID(candidate.Parent)] = true
		}
		return nil, nil
	}
	if len(refs) >= ouroborosMaxResources {
		return nil, fmt.Errorf("resource discovery exceeded %d resources", ouroborosMaxResources)
	}
	if totalOuroborosResourceBytes(refs)+int64(len(data)) > ouroborosMaxTotalResourceBytes {
		return nil, fmt.Errorf("resource capture exceeds total byte limit %d", ouroborosMaxTotalResourceBytes)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return nil, fmt.Errorf("write resource %s: %w", candidate.Ref, err)
	}
	parents := map[string]bool{}
	if candidate.Parent != "" {
		parents[canonicalOuroborosResourceID(candidate.Parent)] = true
	}
	refs[candidate.Ref] = &ouroborosResourceBinding{
		ID:          stableOuroborosResourceID("resource", candidate.Ref),
		Ref:         candidate.Ref,
		File:        outputRel,
		Producer:    producer,
		Kind:        ouroborosResourceKind(candidate.Ref),
		Source:      defaultString(candidate.Source, "html"),
		ContentType: contentType,
		SHA256:      sha,
		Bytes:       int64(len(data)),
		GzipBytes:   gzipLength(data),
		BrotliBytes: brotliLength(data),
		Routes:      map[string]bool{routeID: true},
		Parents:     parents,
	}
	return discoverNestedOuroborosResources(candidate, data, contentType)
}

func totalOuroborosResourceBytes(refs map[string]*ouroborosResourceBinding) int64 {
	var total int64
	for _, ref := range refs {
		if ref != nil {
			total += ref.Bytes
		}
	}
	return total
}

func readBoundedOuroborosResource(r io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := io.CopyN(&buf, r, ouroborosMaxResourceBytes+1); err != nil && err != io.EOF {
		return nil, err
	}
	if int64(buf.Len()) > ouroborosMaxResourceBytes {
		return nil, fmt.Errorf("resource body exceeds %d bytes", ouroborosMaxResourceBytes)
	}
	return buf.Bytes(), nil
}

func localResourceOutputRel(ref string) (string, error) {
	clean := path.Clean("/" + strings.TrimLeft(ref, "/"))
	if clean == "/" || strings.Contains(clean, "\x00") {
		return "", fmt.Errorf("invalid resource path %s", ref)
	}
	return strings.TrimLeft(clean, "/"), nil
}

func discoverNestedOuroborosResources(parent ouroborosResourceCandidate, data []byte, contentType string) ([]ouroborosResourceCandidate, error) {
	if parent.Depth >= ouroborosMaxResourceDepth {
		return nil, nil
	}
	ext := strings.ToLower(path.Ext(parent.Ref))
	values := []string{}
	if ext == ".css" || strings.Contains(strings.ToLower(contentType), "text/css") {
		for _, match := range regexp.MustCompile(`url\(([^)]+)\)`).FindAllStringSubmatch(string(data), -1) {
			values = append(values, strings.TrimSpace(match[1]))
		}
	}
	if ext == ".gltf" || strings.Contains(strings.ToLower(contentType), "model/gltf+json") || strings.Contains(strings.ToLower(contentType), "application/json") {
		var doc any
		if err := json.Unmarshal(data, &doc); err == nil {
			collectGLTFURIValues(doc, &values)
		}
	}
	out := []ouroborosResourceCandidate{}
	for _, value := range values {
		source := "gltf"
		if strings.ToLower(path.Ext(parent.Ref)) == ".css" || strings.Contains(strings.ToLower(contentType), "text/css") {
			source = "css"
		}
		candidate, ok, err := classifyOuroborosResourceURL(value, parent.BaseDir, source, parent.Ref, parent.Depth+1)
		if err != nil {
			return nil, err
		}
		if ok && !candidate.Dynamic {
			out = append(out, candidate)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out, nil
}

func collectGLTFURIValues(value any, out *[]string) {
	switch v := value.(type) {
	case map[string]any:
		for key, nested := range v {
			if key == "uri" {
				if uri, ok := nested.(string); ok {
					*out = append(*out, uri)
				}
				continue
			}
			collectGLTFURIValues(nested, out)
		}
	case []any:
		for _, nested := range v {
			collectGLTFURIValues(nested, out)
		}
	}
}

func sortedOuroborosResourceRefs(refs map[string]*ouroborosResourceBinding) []ouroborosExportResourceRef {
	keys := sortedMapKeys(refs)
	out := make([]ouroborosExportResourceRef, 0, len(keys))
	for _, key := range keys {
		binding := refs[key]
		routes := sortedBoolMapKeys(binding.Routes)
		parents := sortedBoolMapKeys(binding.Parents)
		if len(parents) == 0 {
			parents = nil
		}
		out = append(out, ouroborosExportResourceRef{
			ID:          binding.ID,
			Ref:         binding.Ref,
			File:        binding.File,
			Producer:    binding.Producer,
			Kind:        binding.Kind,
			Source:      binding.Source,
			ContentType: binding.ContentType,
			SHA256:      binding.SHA256,
			Bytes:       binding.Bytes,
			GzipBytes:   binding.GzipBytes,
			BrotliBytes: binding.BrotliBytes,
			Routes:      routes,
			Parents:     parents,
		})
	}
	return out
}

func sortedOuroborosDynamicRefs(refs map[string]*ouroborosDynamicBinding) []ouroborosExportDynamicRef {
	keys := sortedMapKeys(refs)
	out := make([]ouroborosExportDynamicRef, 0, len(keys))
	for _, key := range keys {
		binding := refs[key]
		out = append(out, ouroborosExportDynamicRef{
			Ref:      binding.Ref,
			Producer: binding.Producer,
			Kind:     binding.Kind,
			Reason:   binding.Reason,
			Routes:   sortedBoolMapKeys(binding.Routes),
		})
	}
	return out
}

func sortedBoolMapKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		if values[key] {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func addOuroborosAssetBinding(refs map[string]*ouroborosAssetBinding, ref, producer string, app *ouroborosExportBuiltApp) error {
	src, asset, ok := strictManifestRefSource(app.DistDir, app.Manifest, ref)
	if !ok {
		return fmt.Errorf("asset ref %s is not bound to %s build manifest", ref, app.Name)
	}
	if err := validateOuroborosManifestAsset(ref, src, asset); err != nil {
		return err
	}
	sum, err := fileSHA256Hex(src)
	if err != nil {
		return err
	}
	next := &ouroborosAssetBinding{Producer: producer, App: app, Source: src, Asset: asset, SHA256: sum}
	if existing, ok := refs[ref]; ok {
		if existing.Asset.Hash != next.Asset.Hash || existing.Asset.Size != next.Asset.Size || existing.SHA256 != next.SHA256 {
			return fmt.Errorf("asset ref %s is produced by both %s and %s with different content", ref, existing.Producer, producer)
		}
		return nil
	}
	refs[ref] = next
	return nil
}

func copyStrictOuroborosAssets(outputDir string, refs map[string]*ouroborosAssetBinding) ([]ouroborosExportAssetRef, *buildmanifest.Manifest, error) {
	keys := sortedOuroborosAssetRefs(refs)
	filtered := &buildmanifest.Manifest{}
	copied := make([]ouroborosExportAssetRef, 0, len(keys))
	for _, ref := range keys {
		binding := refs[ref]
		if binding == nil || binding.App == nil {
			return nil, nil, fmt.Errorf("asset ref %s has no producer binding", ref)
		}
		canonicalDst := filepath.Join(outputDir, "assets", binding.AssetBucket(), binding.Asset.File)
		if err := copyFile(canonicalDst, binding.Source); err != nil {
			return nil, nil, err
		}
		if err := copyCompressedSidecars(canonicalDst, binding.Source); err != nil {
			return nil, nil, err
		}
		dst, ok := exportRuntimeOutputPath(outputDir, ref)
		if !ok {
			return nil, nil, fmt.Errorf("asset ref %s has unsafe output path", ref)
		}
		if err := copyFile(dst, binding.Source); err != nil {
			return nil, nil, err
		}
		if err := copyCompressedSidecars(dst, binding.Source); err != nil {
			return nil, nil, err
		}
		if err := validateCopiedAssetAlias(canonicalDst, dst); err != nil {
			return nil, nil, err
		}
		if err := addFilteredManifestAsset(filtered, binding.App.Manifest, ref, filepath.Base(binding.Source)); err != nil {
			return nil, nil, err
		}
		copied = append(copied, ouroborosExportAssetRef{
			Ref:    ref,
			Bucket: bucketForOuroborosRef(ref),
			File:   filepath.Base(binding.Source),
			Hash:   binding.Asset.Hash,
			Size:   binding.Asset.Size,
			SHA256: "sha256:" + binding.SHA256,
		})
	}
	return copied, filtered, nil
}

func (b *ouroborosAssetBinding) AssetBucket() string {
	if b == nil {
		return ""
	}
	return bucketForOuroborosRefFromFile(b.Asset.File, b.App.Manifest)
}

func bucketForOuroborosRefFromFile(file string, manifest *buildmanifest.Manifest) string {
	if _, ok := assetFromManifestBucket(manifest, "runtime", file); ok {
		return "runtime"
	}
	if _, ok := assetFromManifestBucket(manifest, "islands", file); ok {
		return "islands"
	}
	if _, ok := assetFromManifestBucket(manifest, "css", file); ok {
		return "css"
	}
	return bucketForOuroborosRef(file)
}

func validateCopiedAssetAlias(canonicalPath, aliasPath string) error {
	canonical, err := os.ReadFile(canonicalPath)
	if err != nil {
		return err
	}
	alias, err := os.ReadFile(aliasPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, alias) {
		return fmt.Errorf("asset alias %s differs from canonical copy %s", aliasPath, canonicalPath)
	}
	return nil
}

func validateOuroborosManifestAsset(ref, src string, asset buildmanifest.HashedAsset) error {
	if strings.TrimSpace(asset.Hash) == "" {
		return fmt.Errorf("asset ref %s has no manifest hash", ref)
	}
	if filepath.Base(src) != asset.File {
		return fmt.Errorf("asset ref %s resolved file %s but manifest names %s", ref, filepath.Base(src), asset.File)
	}
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("inspect asset %s: %w", ref, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("asset ref %s is not a regular file", ref)
	}
	if asset.Size != info.Size() {
		return fmt.Errorf("asset ref %s size = %d, manifest says %d", ref, info.Size(), asset.Size)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if got := contentHash(data); got != asset.Hash {
		return fmt.Errorf("asset ref %s manifest hash = %s, content hash = %s", ref, asset.Hash, got)
	}
	if !hashedFilenameCarriesHash(asset.File, asset.Hash) {
		return fmt.Errorf("asset ref %s filename %s does not carry manifest hash %s", ref, asset.File, asset.Hash)
	}
	return nil
}

func hashedFilenameCarriesHash(file, hash string) bool {
	base := filepath.Base(file)
	hash = strings.TrimSpace(hash)
	return hash != "" && strings.Contains(base, "."+hash+".")
}

func strictManifestRefSource(distDir string, manifest *buildmanifest.Manifest, ref string) (string, buildmanifest.HashedAsset, bool) {
	src, ok := manifestRuntimeRefSourcePath(distDir, manifest, ref)
	if !ok {
		return "", buildmanifest.HashedAsset{}, false
	}
	asset, ok := manifestAssetForOuroborosRef(manifest, ref, filepath.Base(src))
	if !ok {
		return "", buildmanifest.HashedAsset{}, false
	}
	return src, asset, true
}

func manifestAssetForOuroborosRef(manifest *buildmanifest.Manifest, ref, file string) (buildmanifest.HashedAsset, bool) {
	if manifest == nil {
		return buildmanifest.HashedAsset{}, false
	}
	if rel, ok := strings.CutPrefix(path.Clean("/"+strings.TrimLeft(ref, "/")), "/gosx/assets/"); ok {
		parts := strings.Split(rel, "/")
		if len(parts) == 2 {
			return assetFromManifestBucket(manifest, parts[0], parts[1])
		}
	}
	return assetFromManifestBucket(manifest, bucketForOuroborosRef(ref), file)
}

func assetFromManifestBucket(manifest *buildmanifest.Manifest, bucket, file string) (buildmanifest.HashedAsset, bool) {
	if file == "" {
		return buildmanifest.HashedAsset{}, false
	}
	runtimeAssets := []buildmanifest.HashedAsset{
		manifest.Runtime.WASM, manifest.Runtime.WASMIslands, manifest.Runtime.WASMExec, manifest.Runtime.StandardGoWASMExec,
		manifest.Runtime.Bootstrap, manifest.Runtime.BootstrapLite, manifest.Runtime.BootstrapRuntime,
		manifest.Runtime.BootstrapFeatureIslands, manifest.Runtime.BootstrapFeatureEngines, manifest.Runtime.BootstrapFeatureHubs,
		manifest.Runtime.BootstrapFeatureControllers, manifest.Runtime.BootstrapFeatureTextlayout,
		manifest.Runtime.BootstrapFeatureScene3D, manifest.Runtime.BootstrapFeatureScene3DCommand, manifest.Runtime.BootstrapFeatureScene3DWebGPU,
		manifest.Runtime.BootstrapFeatureScene3DWebGL, manifest.Runtime.BootstrapFeatureScene3DGLTF, manifest.Runtime.BootstrapFeatureScene3DAnimation,
		manifest.Runtime.BootstrapFeatureScene3DCompute, manifest.Runtime.BootstrapFeatureScene3DDecompress,
		manifest.Runtime.Patch, manifest.Runtime.VideoHLS, manifest.Runtime.StripeBridge, manifest.Runtime.Relay, manifest.Runtime.DevtoolsLantern, manifest.Runtime.YouTubeAudio,
	}
	if bucket == "runtime" {
		for _, asset := range runtimeAssets {
			if asset.File == file {
				return asset, true
			}
		}
	}
	if bucket == "islands" {
		for _, asset := range manifest.Islands {
			if asset.File == file {
				return asset.HashedAsset, true
			}
		}
	}
	if bucket == "css" {
		for _, asset := range manifest.CSS {
			if asset.File == file {
				return asset.HashedAsset, true
			}
		}
	}
	return buildmanifest.HashedAsset{}, false
}

func addFilteredManifestAsset(out, src *buildmanifest.Manifest, ref, file string) error {
	bucket := bucketForOuroborosRef(ref)
	switch bucket {
	case "runtime":
		return addFilteredRuntimeAsset(out, src, file)
	case "islands":
		for _, asset := range src.Islands {
			if asset.File == file {
				out.Islands = append(out.Islands, asset)
				return nil
			}
		}
	case "css":
		for _, asset := range src.CSS {
			if asset.File == file {
				out.CSS = append(out.CSS, asset)
				return nil
			}
		}
	}
	return fmt.Errorf("cannot filter build manifest for %s", ref)
}

func addFilteredRuntimeAsset(out, src *buildmanifest.Manifest, file string) error {
	for _, item := range []struct {
		src *buildmanifest.HashedAsset
		dst *buildmanifest.HashedAsset
	}{
		{&src.Runtime.WASM, &out.Runtime.WASM}, {&src.Runtime.WASMIslands, &out.Runtime.WASMIslands}, {&src.Runtime.WASMExec, &out.Runtime.WASMExec},
		{&src.Runtime.StandardGoWASMExec, &out.Runtime.StandardGoWASMExec}, {&src.Runtime.Bootstrap, &out.Runtime.Bootstrap},
		{&src.Runtime.BootstrapLite, &out.Runtime.BootstrapLite}, {&src.Runtime.BootstrapRuntime, &out.Runtime.BootstrapRuntime},
		{&src.Runtime.BootstrapFeatureIslands, &out.Runtime.BootstrapFeatureIslands}, {&src.Runtime.BootstrapFeatureEngines, &out.Runtime.BootstrapFeatureEngines},
		{&src.Runtime.BootstrapFeatureHubs, &out.Runtime.BootstrapFeatureHubs}, {&src.Runtime.BootstrapFeatureControllers, &out.Runtime.BootstrapFeatureControllers},
		{&src.Runtime.BootstrapFeatureTextlayout, &out.Runtime.BootstrapFeatureTextlayout}, {&src.Runtime.BootstrapFeatureScene3D, &out.Runtime.BootstrapFeatureScene3D},
		{&src.Runtime.BootstrapFeatureScene3DCommand, &out.Runtime.BootstrapFeatureScene3DCommand}, {&src.Runtime.BootstrapFeatureScene3DWebGPU, &out.Runtime.BootstrapFeatureScene3DWebGPU},
		{&src.Runtime.BootstrapFeatureScene3DWebGL, &out.Runtime.BootstrapFeatureScene3DWebGL}, {&src.Runtime.BootstrapFeatureScene3DGLTF, &out.Runtime.BootstrapFeatureScene3DGLTF},
		{&src.Runtime.BootstrapFeatureScene3DAnimation, &out.Runtime.BootstrapFeatureScene3DAnimation}, {&src.Runtime.BootstrapFeatureScene3DCompute, &out.Runtime.BootstrapFeatureScene3DCompute},
		{&src.Runtime.BootstrapFeatureScene3DDecompress, &out.Runtime.BootstrapFeatureScene3DDecompress}, {&src.Runtime.Patch, &out.Runtime.Patch},
		{&src.Runtime.VideoHLS, &out.Runtime.VideoHLS}, {&src.Runtime.StripeBridge, &out.Runtime.StripeBridge}, {&src.Runtime.Relay, &out.Runtime.Relay},
		{&src.Runtime.DevtoolsLantern, &out.Runtime.DevtoolsLantern}, {&src.Runtime.YouTubeAudio, &out.Runtime.YouTubeAudio},
	} {
		if item.src.File == file {
			*item.dst = *item.src
			return nil
		}
	}
	return fmt.Errorf("runtime file %s not found in manifest", file)
}

func validateStrictOuroborosExportRoutes(routes []strictOuroborosExportRoute) error {
	if len(routes) != len(canonicalOuroborosExportIDs) {
		return fmt.Errorf("export has %d routes, want 12", len(routes))
	}
	seen := map[string]bool{}
	for i, id := range canonicalOuroborosExportIDs {
		want := map[string]string{"R00": "/static", "R01": "/lite", "R02": "/island/counter", "R03": "/islands/kitchen", "R04": "/action/form", "R05": "/canvas-board", "R06": "/hub/echo", "R07": "/video-sync", "R08": "/scene/basic", "R09A": "/navigation/a", "R09B": "/navigation/b", "R10": "/demos/water"}[id]
		if routes[i].Path != want {
			return fmt.Errorf("export route %d path = %q, want %q", i, routes[i].Path, want)
		}
		if seen[routes[i].Path] {
			return fmt.Errorf("duplicate export route %s", routes[i].Path)
		}
		seen[routes[i].Path] = true
		if strings.TrimSpace(routes[i].File) == "" {
			return fmt.Errorf("export route %s has empty file", routes[i].Path)
		}
		if !strings.HasPrefix(routes[i].SHA256, "sha256:") || routes[i].Bytes <= 0 {
			return fmt.Errorf("export route %s has invalid hash metadata", routes[i].Path)
		}
	}
	return nil
}

func validateOuroborosProducerRoot(root string, assets []ouroborosExportAssetRef, provenance ouroborosExportProvenance) error {
	if err := rejectSymlinksAndValidateOuroborosProducerRoot(root, assets, provenance.ResourceRefs); err != nil {
		return err
	}
	distRoot := filepath.Join(root, "dist")
	manifest, err := loadStrictBuildManifest(filepath.Join(distRoot, "build.json"))
	if err != nil {
		return err
	}
	exportManifest, err := loadStrictOuroborosExportManifest(filepath.Join(distRoot, "export.json"))
	if err != nil {
		return err
	}
	if err := validateStrictOuroborosExportRoutes(exportManifest.Routes); err != nil {
		return err
	}
	if !equalStringSlices(exportManifest.AssetRefs, assetRefsFromProvenance(assets)) {
		return fmt.Errorf("export.json assetRefs differ from copied assets")
	}
	provenancePath := filepath.Join(distRoot, "_ouroboros", "export-corpus.json")
	loadedProvenance, err := loadStrictOuroborosExportProvenance(provenancePath)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(loadedProvenance, provenance) {
		return fmt.Errorf("provenance changed after write")
	}
	if !equalStringSlices(exportManifest.AssetRefs, assetRefsFromProvenance(loadedProvenance.AssetRefs)) {
		return fmt.Errorf("export.json assetRefs differ from provenance")
	}
	if err := validateOuroborosRouteHashes(distRoot, exportManifest.Routes, loadedProvenance.Routes); err != nil {
		return err
	}
	if err := validateOuroborosManifestAssets(distRoot, manifest, loadedProvenance.AssetRefs); err != nil {
		return err
	}
	if err := validateOuroborosResourceManifest(distRoot, loadedProvenance); err != nil {
		return err
	}
	return nil
}

type strictOuroborosExportManifest struct {
	Routes           []strictOuroborosExportRoute `json:"routes"`
	AssetRefs        []string                     `json:"assetRefs,omitempty"`
	ResourceManifest string                       `json:"resourceManifest"`
}

type strictOuroborosExportRoute struct {
	Path         string            `json:"path"`
	File         string            `json:"file"`
	Capabilities routeCapabilities `json:"capabilities"`
	SHA256       string            `json:"sha256"`
	Bytes        int64             `json:"bytes"`
}

func loadStrictOuroborosExportManifest(path string) (strictOuroborosExportManifest, error) {
	var out strictOuroborosExportManifest
	if err := decodeStrictJSONFile(path, &out); err != nil {
		return out, fmt.Errorf("decode export.json: %w", err)
	}
	if len(out.Routes) != len(canonicalOuroborosExportIDs) {
		return out, fmt.Errorf("export.json must contain exact 12 routes")
	}
	if out.ResourceManifest != CanonicalResourceManifestRef {
		return out, fmt.Errorf("export.json resourceManifest = %q, want %q", out.ResourceManifest, CanonicalResourceManifestRef)
	}
	out.AssetRefs = append([]string(nil), out.AssetRefs...)
	sort.Strings(out.AssetRefs)
	return out, nil
}

func loadStrictOuroborosExportProvenance(path string) (ouroborosExportProvenance, error) {
	var out ouroborosExportProvenance
	if err := decodeStrictJSONFile(path, &out); err != nil {
		return out, fmt.Errorf("decode export provenance: %w", err)
	}
	if out.SchemaVersion != ouroborosExportSchemaVersion || out.ContractVersion != "O0.2" {
		return out, fmt.Errorf("unexpected export provenance contract")
	}
	return out, nil
}

type strictOuroborosBuildManifest struct {
	Runtime buildmanifest.RuntimeAssets `json:"runtime"`
	Islands []buildmanifest.IslandAsset `json:"islands"`
	CSS     []buildmanifest.CSSAsset    `json:"css"`
}

func loadStrictBuildManifest(path string) (*buildmanifest.Manifest, error) {
	var mirror strictOuroborosBuildManifest
	if err := decodeStrictJSONFile(path, &mirror); err != nil {
		return nil, fmt.Errorf("decode build.json: %w", err)
	}
	return &buildmanifest.Manifest{
		Runtime: mirror.Runtime,
		Islands: mirror.Islands,
		CSS:     mirror.CSS,
	}, nil
}

func decodeStrictJSONFile(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func validateOuroborosRouteHashes(distRoot string, exportRoutes []strictOuroborosExportRoute, routes []ouroborosExportRoute) error {
	if len(routes) != len(canonicalOuroborosExportIDs) || len(exportRoutes) != len(canonicalOuroborosExportIDs) {
		return fmt.Errorf("provenance must contain exact 12 routes")
	}
	files := map[string]bool{}
	for i, id := range canonicalOuroborosExportIDs {
		route := routes[i]
		if route.ID != id {
			return fmt.Errorf("provenance route %d id = %q, want %q", i, route.ID, id)
		}
		if route.Producer != expectedOuroborosRouteProducer(id) {
			return fmt.Errorf("provenance route %s producer = %q", id, route.Producer)
		}
		if route.File != filepath.ToSlash(buildmanifest.ExportFilePath(route.Path)) {
			return fmt.Errorf("provenance route %s file = %q, want export file for %s", id, route.File, route.Path)
		}
		if files[route.File] {
			return fmt.Errorf("duplicate route HTML file %s", route.File)
		}
		files[route.File] = true
		if exportRoutes[i].Path != route.Path || filepath.ToSlash(exportRoutes[i].File) != route.File {
			return fmt.Errorf("export route %d does not match provenance route %s", i, id)
		}
		if exportRoutes[i].Capabilities != route.Capabilities {
			return fmt.Errorf("export route %s capabilities differ from provenance", id)
		}
		if exportRoutes[i].SHA256 != route.SHA256 || exportRoutes[i].Bytes != route.Bytes {
			return fmt.Errorf("export route %s hash metadata differs from provenance", id)
		}
		htmlPath, err := containedOuroborosOutputPath(distRoot, filepath.FromSlash(route.File))
		if err != nil {
			return err
		}
		if err := validateFileHashAndSize(htmlPath, route.SHA256, route.Bytes); err != nil {
			return fmt.Errorf("route %s HTML: %w", route.ID, err)
		}
		rawPath, err := containedOuroborosOutputPath(filepath.Join(distRoot, "_ouroboros", "raw-html"), route.ID+".html")
		if err != nil {
			return err
		}
		if err := validateFileHashAndSize(rawPath, route.RawSHA256, route.RawBytes); err != nil {
			return fmt.Errorf("route %s raw HTML: %w", route.ID, err)
		}
	}
	return nil
}

func expectedOuroborosRouteProducer(id string) string {
	if id == "R10" {
		return "docs"
	}
	return "fixture"
}

func validateOuroborosManifestAssets(distRoot string, manifest *buildmanifest.Manifest, assets []ouroborosExportAssetRef) error {
	expected := map[string]ouroborosExportAssetRef{}
	for _, asset := range assets {
		key := asset.Bucket + "/" + asset.File
		if existing, exists := expected[key]; exists {
			if existing.Hash != asset.Hash || existing.Size != asset.Size || existing.SHA256 != asset.SHA256 {
				return fmt.Errorf("copied asset %s has conflicting duplicate metadata", key)
			}
		} else {
			expected[key] = asset
		}
		manifestAsset, ok := assetFromManifestBucket(manifest, asset.Bucket, asset.File)
		if !ok {
			return fmt.Errorf("copied asset %s missing from build manifest", key)
		}
		if manifestAsset.Hash != asset.Hash || manifestAsset.Size != asset.Size {
			return fmt.Errorf("copied asset %s does not match build manifest", key)
		}
		canonicalPath := filepath.Join(distRoot, "assets", asset.Bucket, asset.File)
		if err := validateFileHashAndSize(canonicalPath, asset.SHA256, asset.Size); err != nil {
			return fmt.Errorf("canonical asset %s: %w", key, err)
		}
		if got := contentHashFromFileForValidation(canonicalPath); got != asset.Hash {
			return fmt.Errorf("canonical asset %s manifest hash = %s, content hash = %s", key, asset.Hash, got)
		}
		if alias, ok := exportRuntimeOutputPath(distRoot, asset.Ref); ok {
			if err := validateFileHashAndSize(alias, asset.SHA256, asset.Size); err != nil {
				return fmt.Errorf("asset alias %s: %w", asset.Ref, err)
			}
			for _, ext := range []string{".gz", ".br"} {
				if isFile(canonicalPath+ext) || isFile(alias+ext) {
					if err := validateSidecarCopiesMatch(canonicalPath+ext, alias+ext); err != nil {
						return err
					}
				}
			}
		}
	}
	actual, err := manifestAssetKeys(manifest)
	if err != nil {
		return err
	}
	if !equalStringSlices(sortedMapKeys(expected), actual) {
		return fmt.Errorf("build.json contains unreferenced or missing assets")
	}
	return nil
}

func validateOuroborosResourceManifest(distRoot string, provenance ouroborosExportProvenance) error {
	if err := validateOuroborosResourceManifestWithPerf(distRoot); err != nil {
		return fmt.Errorf("shared resource manifest validation: %w", err)
	}
	var manifest ouroborosResourceManifest
	if err := decodeStrictJSONFile(filepath.Join(distRoot, CanonicalResourceManifestRef), &manifest); err != nil {
		return fmt.Errorf("decode resources manifest: %w", err)
	}
	if manifest.SchemaVersion != ResourceManifestSchemaVersion || manifest.Contract != "O0.2" || manifest.CorpusID != provenance.CorpusID {
		return fmt.Errorf("unexpected resources manifest identity")
	}
	if !reflect.DeepEqual(manifest.Resources, resourcesForManifestComparison(provenance.ResourceRefs)) {
		return fmt.Errorf("resources manifest resources differ from provenance")
	}
	if !reflect.DeepEqual(manifest.DynamicEndpoints, dynamicsForManifestComparison(provenance.DynamicRefs)) {
		return fmt.Errorf("resources manifest dynamic endpoints differ from provenance")
	}
	routePaths := canonicalOuroborosRoutePathByID()
	if len(manifest.Routes) != len(canonicalOuroborosExportIDs) {
		return fmt.Errorf("resources manifest must contain exact 12 routes")
	}
	for i, id := range canonicalOuroborosExportIDs {
		if manifest.Routes[i].ID != id || manifest.Routes[i].Route != routePaths[id] {
			return fmt.Errorf("resources manifest route %d = %s/%s", i, manifest.Routes[i].ID, manifest.Routes[i].Route)
		}
	}
	if !reflect.DeepEqual(manifest.DynamicEndpoints, expectedOuroborosDynamicEndpoints()) {
		return fmt.Errorf("resources manifest dynamic endpoints differ from canonical corpus")
	}
	seenURL := map[string]bool{}
	seenOutput := map[string]bool{}
	for _, resource := range provenance.ResourceRefs {
		if seenURL[resource.Ref] {
			return fmt.Errorf("duplicate resource URL %s", resource.Ref)
		}
		seenURL[resource.Ref] = true
		if seenOutput[resource.File] {
			return fmt.Errorf("duplicate resource output path %s", resource.File)
		}
		seenOutput[resource.File] = true
		if !strings.HasPrefix(resource.Ref, "/") || strings.HasPrefix(resource.Ref, "/gosx/") {
			return fmt.Errorf("unsafe resource URL %s", resource.Ref)
		}
		resourcePath, err := containedOuroborosOutputPath(distRoot, filepath.FromSlash(resource.File))
		if err != nil {
			return err
		}
		if err := validateFileHashAndSize(resourcePath, resource.SHA256, resource.Bytes); err != nil {
			return fmt.Errorf("resource %s: %w", resource.Ref, err)
		}
		data, err := os.ReadFile(resourcePath)
		if err != nil {
			return err
		}
		if gzipLength(data) != resource.GzipBytes || brotliLength(data) != resource.BrotliBytes {
			return fmt.Errorf("resource %s compressed metrics changed", resource.Ref)
		}
	}
	return nil
}

func resourcesForManifestComparison(resources []ouroborosExportResourceRef) []ouroborosManifestResource {
	out := make([]ouroborosManifestResource, 0, len(resources))
	for _, resource := range resources {
		out = append(out, ouroborosManifestResource{
			ID:           resource.ID,
			URL:          resource.Ref,
			OutputPath:   resource.File,
			Producer:     resource.Producer,
			Kind:         resource.Kind,
			Source:       resource.Source,
			ContentType:  resource.ContentType,
			SHA256:       resource.SHA256,
			Bytes:        resource.Bytes,
			GzipBytes:    resource.GzipBytes,
			BrotliBytes:  resource.BrotliBytes,
			UsedByRoutes: append([]string(nil), resource.Routes...),
			Parents:      append([]string(nil), resource.Parents...),
		})
	}
	return out
}

func dynamicsForManifestComparison(dynamics []ouroborosExportDynamicRef) []ouroborosDynamicEndpoint {
	routePaths := canonicalOuroborosRoutePathByID()
	out := []ouroborosDynamicEndpoint{}
	for _, dynamic := range dynamics {
		for _, routeID := range dynamic.Routes {
			out = append(out, ouroborosDynamicEndpoint{
				ID:       stableOuroborosResourceID("dynamic", routeID, dynamic.Ref),
				RouteID:  routeID,
				Route:    routePaths[routeID],
				Kind:     dynamic.Kind,
				URL:      dynamic.Ref,
				Producer: dynamic.Producer,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func expectedOuroborosDynamicEndpoints() []ouroborosDynamicEndpoint {
	return dynamicsForManifestComparison(expectedOuroborosDynamicRefs())
}

func expectedOuroborosDynamicRefs() []ouroborosExportDynamicRef {
	return []ouroborosExportDynamicRef{
		{Ref: "/_ouroboros/hub/echo", Producer: "fixture", Kind: "hub", Reason: "dynamic local endpoint", Routes: []string{"R06"}},
		{Ref: "/_ouroboros/video-sync", Producer: "fixture", Kind: "sync", Reason: "dynamic local endpoint", Routes: []string{"R07"}},
		{Ref: "/action/form/__actions/validate-name", Producer: "fixture", Kind: "action", Reason: "dynamic local endpoint", Routes: []string{"R02", "R04"}},
		{Ref: "/lite", Producer: "fixture", Kind: "action", Reason: "dynamic local endpoint", Routes: []string{"R01"}},
	}
}

func manifestAssetKeys(manifest *buildmanifest.Manifest) ([]string, error) {
	seen := map[string]bool{}
	add := func(bucket string, asset buildmanifest.HashedAsset) error {
		if strings.TrimSpace(asset.File) == "" {
			return nil
		}
		key := bucket + "/" + asset.File
		if seen[key] {
			return fmt.Errorf("duplicate manifest asset %s", key)
		}
		seen[key] = true
		return nil
	}
	runtimeAssets := []buildmanifest.HashedAsset{
		manifest.Runtime.WASM, manifest.Runtime.WASMIslands, manifest.Runtime.WASMExec, manifest.Runtime.StandardGoWASMExec,
		manifest.Runtime.Bootstrap, manifest.Runtime.BootstrapLite, manifest.Runtime.BootstrapRuntime,
		manifest.Runtime.BootstrapFeatureIslands, manifest.Runtime.BootstrapFeatureEngines, manifest.Runtime.BootstrapFeatureHubs,
		manifest.Runtime.BootstrapFeatureControllers, manifest.Runtime.BootstrapFeatureTextlayout, manifest.Runtime.BootstrapFeatureScene3D,
		manifest.Runtime.BootstrapFeatureScene3DCommand, manifest.Runtime.BootstrapFeatureScene3DWebGPU, manifest.Runtime.BootstrapFeatureScene3DWebGL,
		manifest.Runtime.BootstrapFeatureScene3DGLTF, manifest.Runtime.BootstrapFeatureScene3DAnimation, manifest.Runtime.BootstrapFeatureScene3DCompute,
		manifest.Runtime.BootstrapFeatureScene3DDecompress, manifest.Runtime.Patch, manifest.Runtime.VideoHLS, manifest.Runtime.StripeBridge,
		manifest.Runtime.Relay, manifest.Runtime.DevtoolsLantern, manifest.Runtime.YouTubeAudio,
	}
	for _, asset := range runtimeAssets {
		if err := add("runtime", asset); err != nil {
			return nil, err
		}
	}
	for _, asset := range manifest.Islands {
		if err := add("islands", asset.HashedAsset); err != nil {
			return nil, err
		}
	}
	for _, asset := range manifest.CSS {
		if err := add("css", asset.HashedAsset); err != nil {
			return nil, err
		}
	}
	return sortedMapKeys(seen), nil
}

func validateFileHashAndSize(path, wantHash string, wantSize int64) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if int64(len(data)) != wantSize {
		return fmt.Errorf("size = %d, want %d", len(data), wantSize)
	}
	sum := sha256.Sum256(data)
	got := "sha256:" + hex.EncodeToString(sum[:])
	if got != wantHash {
		return fmt.Errorf("sha256 = %s, want %s", got, wantHash)
	}
	return nil
}

func contentHashFromFileForValidation(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return contentHash(data)
}

func validateSidecarCopiesMatch(canonical, alias string) error {
	left, err := os.ReadFile(canonical)
	if err != nil {
		return err
	}
	right, err := os.ReadFile(alias)
	if err != nil {
		return err
	}
	if !bytes.Equal(left, right) {
		return fmt.Errorf("asset sidecar alias %s differs from canonical sidecar %s", alias, canonical)
	}
	return nil
}

func assetRefsFromProvenance(assets []ouroborosExportAssetRef) []string {
	out := make([]string, 0, len(assets))
	for _, asset := range assets {
		out = append(out, asset.Ref)
	}
	sort.Strings(out)
	return out
}

func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containedOuroborosOutputPath(root, rel string) (string, error) {
	root = filepath.Clean(root)
	target := filepath.Clean(filepath.Join(root, rel))
	if !sameOrContainedPath(target, root) {
		return "", fmt.Errorf("path escapes output root: %s", rel)
	}
	return target, nil
}

func rejectSymlinksAndValidateOuroborosProducerRoot(root string, assets []ouroborosExportAssetRef, resources []ouroborosExportResourceRef) error {
	distRoot := filepath.Join(root, "dist")
	allowed := map[string]bool{
		filepath.ToSlash(filepath.Join("dist", "build.json")):                       true,
		filepath.ToSlash(filepath.Join("dist", "export.json")):                      true,
		filepath.ToSlash(filepath.Join("dist", "_ouroboros", "export-corpus.json")): true,
		filepath.ToSlash(filepath.Join("dist", CanonicalResourceManifestRef)):       true,
	}
	for _, id := range canonicalOuroborosExportIDs {
		routePath := map[string]string{"R00": "/static", "R01": "/lite", "R02": "/island/counter", "R03": "/islands/kitchen", "R04": "/action/form", "R05": "/canvas-board", "R06": "/hub/echo", "R07": "/video-sync", "R08": "/scene/basic", "R09A": "/navigation/a", "R09B": "/navigation/b", "R10": "/demos/water"}[id]
		allowed[filepath.ToSlash(filepath.Join("dist", buildmanifest.ExportFilePath(routePath)))] = true
		allowed[filepath.ToSlash(filepath.Join("dist", "_ouroboros", "raw-html", id+".html"))] = true
	}
	for _, asset := range assets {
		canonical := filepath.Join(distRoot, "assets", asset.Bucket, asset.File)
		canonicalRel := filepath.ToSlash(relOrAbs(root, canonical))
		allowed[canonicalRel] = true
		for _, ext := range []string{".gz", ".br"} {
			if isFile(canonical + ext) {
				allowed[canonicalRel+ext] = true
			}
		}
		if out, ok := exportRuntimeOutputPath(distRoot, asset.Ref); ok {
			rel := filepath.ToSlash(relOrAbs(root, out))
			allowed[rel] = true
			for _, ext := range []string{".gz", ".br"} {
				if isFile(out + ext) {
					allowed[rel+ext] = true
				}
			}
		}
	}
	for _, resource := range resources {
		resourcePath := filepath.Join(distRoot, filepath.FromSlash(resource.File))
		allowed[filepath.ToSlash(relOrAbs(root, resourcePath))] = true
	}
	return filepath.WalkDir(root, func(p string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("canonical export rejects symlink: %s", p)
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("canonical export rejects non-regular file: %s", p)
		}
		rel := filepath.ToSlash(relOrAbs(root, p))
		if !allowed[rel] {
			return fmt.Errorf("canonical export contains unexpected file: %s", rel)
		}
		return nil
	})
}

func resolveContainedRepoPath(root, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("empty path")
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve repo root symlinks: %w", err)
	}
	rootReal = filepath.Clean(rootReal)
	var candidate string
	if filepath.IsAbs(value) {
		candidate = filepath.Clean(value)
	} else {
		candidate = filepath.Clean(filepath.Join(rootReal, value))
	}
	real, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve %s symlinks: %w", value, err)
	}
	real = filepath.Clean(real)
	if !sameOrContainedPath(real, rootReal) {
		return "", fmt.Errorf("path escapes repository root: %s", value)
	}
	return real, nil
}

func resolveContainedNewRepoOutputPath(root, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("canonical export --out is required")
	}
	root = filepath.Clean(root)
	out, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve out dir: %w", err)
	}
	out = filepath.Clean(out)
	if !sameOrContainedPath(out, root) {
		return "", fmt.Errorf("canonical export --out must stay inside repo root: %s", out)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve repo root symlinks: %w", err)
	}
	realRoot = filepath.Clean(realRoot)
	nearest := filepath.Dir(out)
	for {
		info, statErr := os.Lstat(nearest)
		if statErr == nil {
			if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				return "", fmt.Errorf("canonical export --out parent is not a directory: %s", nearest)
			}
			break
		}
		if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("inspect canonical export --out parent: %w", statErr)
		}
		next := filepath.Dir(nearest)
		if next == nearest {
			return "", fmt.Errorf("canonical export --out parent does not exist under repo root: %s", filepath.Dir(out))
		}
		nearest = next
	}
	realNearest, err := filepath.EvalSymlinks(nearest)
	if err != nil {
		return "", fmt.Errorf("resolve canonical export --out parent symlinks: %w", err)
	}
	if !sameOrContainedPath(filepath.Clean(realNearest), realRoot) {
		return "", fmt.Errorf("canonical export --out parent escapes repo root: %s", nearest)
	}
	return out, nil
}

func resolveCanonicalAppPath(root, value, expectedRel string) (string, error) {
	got, err := resolveContainedRepoPath(root, value)
	if err != nil {
		return "", err
	}
	want, err := resolveContainedRepoPath(root, expectedRel)
	if err != nil {
		return "", err
	}
	if got != want {
		return "", fmt.Errorf("%s must resolve exactly to %s", value, expectedRel)
	}
	return got, nil
}

func sortedOuroborosAssetRefs(refs map[string]*ouroborosAssetBinding) []string {
	out := make([]string, 0, len(refs))
	for ref := range refs {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func bucketForOuroborosRef(ref string) string {
	clean := path.Clean("/" + strings.TrimLeft(strings.TrimSpace(ref), "/"))
	if rel, ok := strings.CutPrefix(clean, "/gosx/assets/"); ok {
		if bucket, _, ok := strings.Cut(rel, "/"); ok {
			return bucket
		}
	}
	if strings.HasPrefix(clean, "/gosx/islands/") {
		return "islands"
	}
	if strings.HasPrefix(clean, "/gosx/css/") {
		return "css"
	}
	return "runtime"
}

func fileSHA256Hex(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func relOrAbs(root, target string) string {
	rel, err := filepath.Rel(root, target)
	if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return target
	}
	return rel
}

func sameCleanSlashPath(a, b string) bool {
	return path.Clean("/"+strings.Trim(strings.TrimSpace(filepath.ToSlash(a)), "/")) == path.Clean("/"+strings.Trim(strings.TrimSpace(filepath.ToSlash(b)), "/"))
}
