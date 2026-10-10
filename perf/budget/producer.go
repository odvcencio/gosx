package budget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/assetmeasure"
	"m31labs.dev/gosx/internal/pagecaps"
)

// ProducerOptions are private bindings for a completed production build.
// ProduceFixture snapshots documents from its running server and verifies every
// supplied build body. It neither starts servers nor derives budgets.
type ProducerOptions struct {
	Inputs                           *Inputs                 `json:"-"`
	Build                            *buildmanifest.Manifest `json:"-"`
	App, DistDir, BaseURL, SourceSHA string                  `json:"-"`
	Client                           *http.Client            `json:"-"`
}

type producerAssetRule struct {
	ID           string   `json:"id"`
	Owner        string   `json:"owner"`
	Kind         string   `json:"kind"`
	Phase        string   `json:"phase"`
	Condition    string   `json:"condition"`
	Dependencies []string `json:"dependencies"`
}

type producerCatalog struct {
	Schema              string              `json:"schema"`
	Version             int64               `json:"version"`
	Routes              []FixtureRoute      `json:"routes"`
	AssetRules          []producerAssetRule `json:"assetRules"`
	InteractionContract Ref                 `json:"interactionContract"`
}

func (rule producerAssetRule) assetUse(url string) buildmanifest.PerfAssetUse {
	// Only graph metadata is validated at preflight. Body verification replaces
	// this provisional hash before the manifest can be serialized or published.
	return buildmanifest.PerfAssetUse{ID: rule.ID, SHA256: strings.Repeat("0", 64), URL: url,
		Owner: rule.Owner, Kind: rule.Kind, Phase: rule.Phase, Condition: rule.Condition,
		Dependencies: append([]string{}, rule.Dependencies...)}
}

// ProduceFixture writes the private fixture contract and returns its independent
// producer proof. Callers retain that proof outside the manifest when collecting.
func ProduceFixture(ctx context.Context, opts ProducerOptions) (string, error) {
	fail := func(pointer string) error {
		return &InputError{Code: "wrong-fixture", Reference: "producer", Pointer: pointer}
	}
	if ctx == nil || opts.Inputs == nil || opts.Build == nil || opts.Build.PerfAssetUses == nil || validateInput(opts.SourceSHA, inputDefinitions["Commit"]) != nil {
		return "", fail("/inputs")
	}
	if ctx.Err() != nil {
		return "", fail("/context")
	}
	// The build inventory is only part of the emitted graph. Check its shape
	// now; validate edges after catalog assets and registered app edges join it.
	if opts.Build.PerfAssetUses.Version != 1 || opts.Build.PerfAssetUses.Assets == nil || len(opts.Build.PerfAssetUses.Assets) > 4096 {
		return "", fail("/build")
	}
	data, err := readReference(opts.Inputs.RootDir(), opts.Inputs.File.Fixtures, maxInputBytes)
	if err != nil {
		return "", inputReference(err, "producer", "/catalog")
	}
	var catalog producerCatalog
	var checked json.RawMessage
	if err := decodeInput(data, "FixtureCatalog", &checked); err != nil {
		return "", inputReference(err, "producer", "/catalog")
	}
	if err := json.Unmarshal(checked, &catalog); err != nil {
		return "", fail("/catalog")
	}
	routes := []FixtureRoute{}
	expected := map[string]bool{}
	for _, r := range opts.Inputs.File.Routes {
		if r.App == opts.App {
			expected[r.RouteTemplate] = true
		}
	}
	for _, r := range catalog.Routes {
		if r.App == opts.App && expected[r.RouteTemplate] {
			routes = append(routes, r)
		}
	}
	if len(routes) == 0 || len(routes) != len(expected) {
		return "", fail("/app")
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].RouteTemplate < routes[j].RouteTemplate })
	allowed := map[string]string{}
	rules := map[string]producerAssetRule{}
	for _, r := range catalog.AssetRules {
		allowed[r.ID] = r.Owner + "|" + r.Kind
		rules[r.ID] = r
	}
	base, err := url.Parse(opts.BaseURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return "", fail("/base")
	}
	root, err := os.OpenRoot(opts.DistDir)
	if err != nil {
		return "", fail("/dist")
	}
	defer root.Close()
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	owned := *client
	owned.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if owned.Timeout == 0 {
		owned.Timeout = 30 * time.Second
	}
	manifest := FixtureManifest{Schema: "gosx.perf-fixtures/v1", Version: 1, SourceSHA: opts.SourceSHA, CatalogSHA256: opts.Inputs.File.Fixtures.SHA256, Routes: routes, Assets: []buildmanifest.PerfAssetUse{}}
	seen := map[string]bool{}
	for _, use := range opts.Build.PerfAssetUses.Assets {
		raw, marshalErr := json.Marshal(use)
		var checkedUse buildmanifest.PerfAssetUse
		if marshalErr != nil || json.Unmarshal(raw, &checkedUse) != nil {
			return "", fail("/build/assets")
		}
		if ctx.Err() != nil {
			return "", fail("/context")
		}
		if seen[use.ID] || use.Owner == "app" && !strings.HasPrefix(use.ID, "app/"+opts.App+"/") {
			return "", fail("/build/assets")
		}
		if use.Owner == "app" && allowed[use.ID] != use.Owner+"|"+use.Kind {
			return "", fail("/build/assets")
		}
		body, _, err := readFixtureBody(root, use.URL, use.Kind)
		if err != nil || producerHash(body) != use.SHA256 {
			return "", fail("/build/assets/body")
		}
		if use.Kind == "wasm" {
			name := strings.TrimSuffix(strings.TrimPrefix(use.ID, "framework/runtime/"), ".wasm")
			proof, ok := opts.Build.Runtime.WASMOptimization[name]
			if !ok || !proof.Applied || proof.Tool != "wasm-opt" || proof.Version != opts.Inputs.Toolchain.Binaryen || proof.OutputSHA256 != use.SHA256 || !shaPattern.MatchString(proof.InputSHA256) {
				return "", fail("/build/optimizer")
			}
		}
		dependencies := use.Dependencies
		if use.Owner == "app" {
			dependencies = rules[use.ID].Dependencies
		}
		use.Dependencies = append([]string{}, dependencies...)
		manifest.Assets = append(manifest.Assets, use)
		seen[use.ID] = true
	}
	// Public files are whole app-owned bodies. Their catalog IDs bind their native
	// file locations without accepting an arbitrary filesystem walk.
	prefix := "app/" + opts.App + "/public/"
	public := []producerPublicFile{}
	for _, rule := range catalog.AssetRules {
		if !strings.HasPrefix(rule.ID, prefix) {
			continue
		}
		relative := strings.TrimPrefix(rule.ID, "app/"+opts.App+"/")
		info, err := root.Stat(relative)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || seen[rule.ID] || rule.Owner != "app" {
			return "", fail("/public/body")
		}
		urlPath := "/" + strings.TrimPrefix(relative, "public/")
		target, err := fixtureFilePath(urlPath, rule.Kind)
		if err != nil {
			return "", fail("/public/destination")
		}
		// Plan the complete graph before publishing bodies.
		public = append(public, producerPublicFile{source: relative, target: target, assetIndex: len(manifest.Assets)})
		manifest.Assets = append(manifest.Assets, rule.assetUse(urlPath))
		seen[rule.ID] = true
	}
	// Validate all document identities before writing any snapshot, including
	// public files. A document is app-owned just like a build or public body.
	documents := map[string]int{}
	for _, route := range routes {
		id := ""
		for _, critical := range route.CriticalAssetIDs {
			if allowed[critical] == "app|html" {
				if id != "" {
					return "", fail("/routes/document")
				}
				id = critical
			}
		}
		if !strings.HasPrefix(id, "app/"+opts.App+"/") || seen[id] {
			return "", fail("/routes/document")
		}
		documents[route.RouteTemplate] = len(manifest.Assets)
		manifest.Assets = append(manifest.Assets, rules[id].assetUse(route.RouteTemplate))
		seen[id] = true
	}
	// Every edge must name an emitted identity already checked against the app
	// and ownership rules above. Validate missing edges, cycles and conditions
	// on the combined build/public/document graph before the first output write.
	if err := (&buildmanifest.Manifest{PerfAssetUses: &buildmanifest.PerfAssetUses{Version: 1, Assets: manifest.Assets}}).ValidatePerfAssetUses(); err != nil {
		return "", fail("/assets/dependencies")
	}
	for _, route := range routes {
		for _, id := range route.CriticalAssetIDs {
			if !seen[id] {
				return "", fail("/routes/criticalAssetIDs")
			}
		}
	}
	boundInputs, err := producerBoundInputPaths(opts, checked)
	if err != nil {
		return "", err
	}
	protection, err := preflightProducerPaths(root, opts, routes, public, boundInputs)
	if err != nil {
		return "", err
	}
	for _, entry := range public {
		body, err := readMeasureFile(root, entry.source, maxMeasureBody)
		if err != nil {
			return "", fail("/public/body")
		}
		manifest.Assets[entry.assetIndex].SHA256 = producerHash(body)
		// Measurement paths are rooted in the fixture directory. Copy public bodies
		// there while leaving the production server's own public tree intact.
		if err := writeProducerFile(root, protection, entry.target, body); err != nil {
			return "", err
		}
		if err := copyProducerSidecars(root, protection, entry.source, entry.target, body); err != nil {
			return "", err
		}
	}
	for _, route := range routes {
		if !validRoute(route.RouteTemplate) || strings.ContainsAny(route.RouteTemplate, "[]") {
			return "", fail("/routes/template")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.ResolveReference(&url.URL{Path: route.RouteTemplate}).String(), nil)
		if err != nil {
			return "", fail("/routes/request")
		}
		req.Header.Set("Accept-Encoding", "identity")
		req.Header.Set("Cache-Control", "no-cache")
		response, err := owned.Do(req)
		if err != nil {
			return "", fail("/routes/request")
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxMeasureBody+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || len(body) > maxMeasureBody || response.StatusCode != http.StatusOK || response.Uncompressed || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/html") {
			return "", fail("/routes/response")
		}
		caps, err := pagecaps.FromHTML(body)
		if err != nil {
			return "", fail("/routes/capabilities")
		}
		observed, _ := json.Marshal(caps)
		declared, _ := json.Marshal(route.Capabilities)
		families, classErr := pagecaps.Classify(caps, false)
		if !bytes.Equal(observed, declared) || classErr != nil || !fixtureCoversTypes(route.PageTypes, families) {
			return "", fail("/routes/capabilities")
		}
		manifest.Assets[documents[route.RouteTemplate]].SHA256 = producerHash(body)
		file, err := fixtureFilePath(route.RouteTemplate, "html")
		if err != nil {
			return "", fail("/routes/document")
		}
		if err := writeProducerFile(root, protection, file, body); err != nil {
			return "", err
		}
		// Prerendered release encodings differ from live HTML compression. Retain
		// them only when the static document is exactly the fetched snapshot.
		static := "static/" + file
		if _, err := root.Stat(static); err == nil {
			built, err := readMeasureFile(root, static, maxMeasureBody)
			if err != nil {
				return "", fail("/routes/document")
			}
			if bytes.Equal(built, body) {
				if err := copyProducerSidecars(root, protection, static, file, body); err != nil {
					return "", err
				}
			}
		} else if !os.IsNotExist(err) {
			return "", fail("/routes/document")
		}
	}
	sort.Slice(manifest.Assets, func(i, j int) bool { return manifest.Assets[i].ID < manifest.Assets[j].ID })
	digest, err := FixtureManifestSHA256(manifest)
	if err != nil {
		return "", inputReference(err, "producer", "/manifest")
	}
	manifest.FixturesSHA256 = digest
	data, err = json.Marshal(manifest)
	if err != nil {
		return "", fail("/manifest")
	}
	if _, err := DecodeFixtureManifest(bytes.NewReader(data)); err != nil {
		return "", inputReference(err, "producer", "/manifest")
	}
	if err := writeProducerFile(root, protection, fixtureManifestFile, append(data, '\n')); err != nil {
		return "", err
	}
	return digest, nil
}

func producerHash(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }

func copyProducerSidecars(root *os.Root, protection *producerPathProtection, source, target string, body []byte) error {
	for _, encoding := range fixtureSidecars {
		_, err := root.Stat(source + encoding.suffix)
		if os.IsNotExist(err) {
			continue
		}
		encoded, readErr := readMeasureFile(root, source+encoding.suffix, maxMeasureBody)
		if err != nil || readErr != nil || assetmeasure.VerifySidecar(body, encoded, encoding.encoding) != nil {
			return &InputError{Code: "stale-sidecar", Reference: "producer", Pointer: "/encoding"}
		}
		if err := writeProducerBytes(root, protection, target+encoding.suffix, encoded); err != nil {
			return err
		}
	}
	return nil
}

func writeProducerFile(root *os.Root, protection *producerPathProtection, name string, data []byte) error {
	// Preflight cleanup too, so a colliding sidecar cannot cause a partial write.
	for _, sidecar := range fixtureSidecars {
		if err := protection.check(root, name+sidecar.suffix); err != nil {
			return err
		}
	}
	if err := writeProducerBytes(root, protection, name, data); err != nil {
		return err
	}
	// A fresh snapshot cannot retain encodings from a different body.
	for _, sidecar := range fixtureSidecars {
		if err := protection.check(root, name+sidecar.suffix); err != nil {
			return err
		}
		if err := root.Remove(name + sidecar.suffix); err != nil && !os.IsNotExist(err) {
			return &InputError{Code: "write-failed", Reference: "producer", Pointer: "/output"}
		}
	}
	return nil
}

func writeProducerBytes(root *os.Root, protection *producerPathProtection, name string, data []byte) error {
	fail := func() error { return &InputError{Code: "write-failed", Reference: "producer", Pointer: "/output"} }
	if !safePath(name) {
		return fail()
	}
	for _, file := range []string{name, name + ".new"} {
		if err := protection.check(root, file); err != nil {
			return err
		}
	}
	if err := root.MkdirAll(path.Dir(name), 0700); err != nil {
		return fail()
	}
	pending := name + ".new"
	f, err := root.OpenFile(pending, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fail()
	}
	defer root.Remove(pending)
	n, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil || n != len(data) {
		return fail()
	}
	if err := protection.check(root, name); err != nil {
		return err
	}
	if err := root.Rename(pending, name); err != nil {
		return fail()
	}
	return nil
}
