package budget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"m31labs.dev/gosx/internal/assetmeasure"
)

// CollectionBinding supplies private serving locations for a registered app.
// Neither paths nor URLs become report fields.
type CollectionBinding struct {
	App     string `json:"-"`
	DistDir string `json:"-"`
	BaseURL string `json:"-"`
	// The production builder supplies this proof separately from the manifest.
	ArtifactSHA256 string `json:"-"`
}
type CollectOptions struct {
	Inputs     *Inputs             `json:"-"`
	Bindings   []CollectionBinding `json:"-"`
	SHA        string              `json:"-"`
	Base       *Report             `json:"-"`
	ChunksOnly bool                `json:"-"`
	Client     *http.Client        `json:"-"`
}

// Collect measures a complete production fixture set without starting servers.
// It returns observations, not a budget decision or a timing certification.
func Collect(ctx context.Context, opts CollectOptions) (*Report, error) {
	t := opts.Inputs
	if t == nil {
		return nil, collectionFailure("invalid-input", "/inputs")
	}
	pin := assetmeasure.CompressorPin{GoVersion: t.Toolchain.Go, BrotliVersion: t.Toolchain.Brotli, GzipLevel: t.Toolchain.GzipLevel, BrotliQuality: t.Toolchain.BrotliQuality, BrotliWindow: t.Toolchain.BrotliWindow}
	if _, err := assetmeasure.Measure(nil, pin); err != nil {
		return nil, collectionFailure("noncanonical", "/pin")
	}
	return collect(ctx, opts, pin, Measure, func(body []byte) (assetmeasure.Sizes, error) { return assetmeasure.Measure(body, pin) })
}

type collectionMeasure func(context.Context, MeasureOptions) (AppReport, error)

func collectionFailure(code, pointer string) error {
	return &InputError{Code: code, Reference: "collection", Pointer: pointer}
}

func collect(ctx context.Context, opts CollectOptions, pin assetmeasure.CompressorPin, measure collectionMeasure, normalize bodyNormalizer) (*Report, error) {
	if ctx == nil || opts.Inputs == nil || validateInput(opts.SHA, inputDefinitions["Commit"]) != nil {
		return nil, collectionFailure("invalid-input", "/sha")
	}
	if ctx.Err() != nil {
		return nil, collectionFailure("timeout", "")
	}
	file := opts.Inputs.File
	apps := map[string]bool{}
	for _, route := range file.Routes {
		apps[route.App] = true
	}
	if len(opts.Bindings) != len(apps) {
		return nil, collectionFailure("wrong-fixture", "/bindings")
	}
	bindings := append([]CollectionBinding{}, opts.Bindings...)
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].App < bindings[j].App })
	seen := map[string]bool{}
	for i, binding := range bindings {
		if !apps[binding.App] || seen[binding.App] || binding.DistDir == "" || validateInput(binding.ArtifactSHA256, inputDefinitions["SHA"]) != nil || !opts.ChunksOnly && binding.BaseURL == "" {
			return nil, collectionFailure("wrong-fixture", "/bindings/"+strconv.Itoa(i))
		}
		seen[binding.App] = true
	}
	runner := "cpu-linux"
	if runtime.GOOS == "windows" {
		runner = "cpu-windows"
	} else if runtime.GOOS != "linux" {
		return nil, collectionFailure("unsupported", "/runner")
	}
	info := PublicInfo{SHA: opts.SHA, ProfileSHA256: file.Profile.SHA256, CoefficientSHA256: file.Coefficients.SHA256, ToolchainSHA256: file.Toolchain.SHA256, FixtureSHA256: file.Fixtures.SHA256, Runner: runner, Canonical: true, Transport: "local-h1", Headless: true, Muted: true, RendererClass: "none"}
	if opts.Base != nil {
		base, err := checkReportInput(opts.Base, "base")
		if err != nil {
			return nil, err
		}
		if base.Info.ArtifactSHA256 == nil {
			return nil, collectionFailure("noncanonical", "/base/info/artifactSHA256")
		}
		info.BaseSHA = base.Info.SHA
		info.BaseArtifactSHA256 = *base.Info.ArtifactSHA256
		info.EpochSHA256 = base.Info.EpochSHA256
		info.Backend = base.Info.Backend
	}
	client := opts.Client
	if client == nil {
		template, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, collectionFailure("environment", "/client")
		}
		transport := template.Clone()
		transport.ForceAttemptHTTP2 = false
		defer transport.CloseIdleConnections()
		client = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	}
	out := &Report{Schema: "gosx.budget-report/v1", Info: info, Mode: "report-only", Rows: []Row{}, Assets: []AssetReport{}, ExceptionIDs: []string{}, Acknowledgments: []Ack{}, Violations: []CountReason{}, Coverage: ByteCoverage{Reachability: "known"}}
	type artifact struct {
		App    string `json:"app"`
		SHA256 string `json:"sha256"`
	}
	artifacts := []artifact{}
	assetIndexes := map[string]int{}
	for i, binding := range bindings {
		if ctx.Err() != nil {
			return nil, collectionFailure("timeout", "")
		}
		root, err := os.OpenRoot(binding.DistDir)
		if err != nil {
			return nil, collectionFailure("wrong-fixture", "/bindings/"+strconv.Itoa(i)+"/dist")
		}
		data, readErr := readMeasureFile(root, fixtureManifestFile, maxInputBytes)
		if readErr != nil {
			root.Close()
			return nil, readErr
		}
		manifest, err := DecodeFixtureManifest(bytes.NewReader(data))
		if err != nil {
			root.Close()
			return nil, err
		}
		if manifest.SourceSHA != opts.SHA || manifest.CatalogSHA256 != file.Fixtures.SHA256 || manifest.FixturesSHA256 != binding.ArtifactSHA256 {
			root.Close()
			return nil, collectionFailure("wrong-fixture", "/manifest/provenance")
		}
		artifacts = append(artifacts, artifact{binding.App, manifest.FixturesSHA256})
		appInfo := info
		digest := manifest.FixturesSHA256
		appInfo.ArtifactSHA256 = &digest
		options := MeasureOptions{App: binding.App, DistDir: binding.DistDir, BaseURL: binding.BaseURL, Client: client, Pin: pin, Public: appInfo}
		var measured AppReport
		if opts.ChunksOnly {
			measured, err = collectChunks(root, binding.App, manifest, normalize)
		} else {
			root.Close()
			root = nil
			measured, err = measure(ctx, options)
		}
		if root != nil {
			root.Close()
		}
		if err != nil {
			return nil, err
		}
		out.Rows = append(out.Rows, measured.Rows...)
		out.Coverage.RoutesExpected += measured.Coverage.RoutesExpected
		out.Coverage.RoutesMeasured += measured.Coverage.RoutesMeasured
		if measured.Coverage.Reachability != "known" {
			out.Coverage.Reachability = "unknown"
		}
		for _, asset := range measured.Assets {
			if previous, ok := assetIndexes[asset.ID]; ok {
				old := &out.Assets[previous]
				if old.Owner != "framework" || asset.Owner != "framework" || old.SHA256 != asset.SHA256 || old.Raw != asset.Raw || old.Gzip != asset.Gzip || old.Brotli != asset.Brotli || old.Kind != asset.Kind || old.Condition != asset.Condition || !equalStrings(old.Dependencies, asset.Dependencies) {
					return nil, collectionFailure("wrong-fixture", "/assets")
				}
				if phaseRank(asset.Phase) < phaseRank(old.Phase) {
					old.Phase = asset.Phase
				}
				continue
			}
			assetIndexes[asset.ID] = len(out.Assets)
			out.Assets = append(out.Assets, asset)
		}
	}
	out.Coverage.AssetsExpected = int64(len(out.Assets))
	out.Coverage.AssetsMeasured = out.Coverage.AssetsExpected
	if opts.ChunksOnly {
		out.Mode = "chunks-only"
		out.Coverage.RoutesExpected = int64(len(file.Routes))
		out.Violations = []CountReason{{ReasonCode: "unknown-reachability", Count: 1}}
	}
	sort.Slice(out.Rows, func(i, j int) bool { return growthRowKey(out.Rows[i]) < growthRowKey(out.Rows[j]) })
	sort.Slice(out.Assets, func(i, j int) bool { return out.Assets[i].ID < out.Assets[j].ID })
	digest := ""
	if len(artifacts) == 1 {
		digest = artifacts[0].SHA256
	} else {
		data, _ := json.Marshal(artifacts)
		sum := sha256.Sum256(append(data, '\n'))
		digest = hex.EncodeToString(sum[:])
	}
	out.Info.ArtifactSHA256 = &digest
	return out, nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := append([]string{}, a...), append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func collectChunks(root *os.Root, app string, manifest *FixtureManifest, normalize bodyNormalizer) (AppReport, error) {
	out := AppReport{App: app, Rows: []Row{}, Assets: []AssetReport{}, Coverage: ByteCoverage{Reachability: "unknown"}}
	physical := map[string]int{}
	for _, use := range manifest.Assets {
		if use.Owner == "app" && !strings.HasPrefix(use.ID, "app/"+app+"/") {
			continue
		}
		body, _, err := readFixtureBody(root, use.URL, use.Kind)
		if err != nil {
			return out, err
		}
		sizes, err := normalize(body)
		if err != nil || sizes.SHA256 != use.SHA256 {
			return out, collectionFailure("wrong-fixture", "/assets/body")
		}
		if previous, ok := physical[use.URL]; ok {
			if use.Owner == "framework" {
				out.Assets[previous].ID = use.ID
				out.Assets[previous].Owner = use.Owner
				out.Assets[previous].Dependencies = append([]string{}, use.Dependencies...)
			}
			continue
		}
		physical[use.URL] = len(out.Assets)
		out.Assets = append(out.Assets, AssetReport{ID: use.ID, SHA256: use.SHA256, Owner: use.Owner, Phase: "startup", Raw: sizes.Raw, Gzip: sizes.Gzip, Brotli: sizes.Brotli, ChangedSources: []string{}, App: app, Kind: use.Kind, Condition: use.Condition, Dependencies: append([]string{}, use.Dependencies...)})
	}
	out.Coverage.AssetsExpected = int64(len(out.Assets))
	out.Coverage.AssetsMeasured = out.Coverage.AssetsExpected
	return out, nil
}
