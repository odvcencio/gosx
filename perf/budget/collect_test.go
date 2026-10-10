package budget

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/assetmeasure"
)

func testCollection(t *testing.T) (CollectOptions, *FixtureManifest) {
	t.Helper()
	inputs, err := LoadInputs("testdata/budget.v2.json", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	measured, manifest, _, _ := testRouteMeasurement(t)
	manifest.CatalogSHA256 = inputs.File.Fixtures.SHA256
	writeTestFixtureManifest(t, measured.DistDir, manifest)
	opts := CollectOptions{Inputs: inputs, SHA: manifest.SourceSHA, Client: measured.Client, Bindings: []CollectionBinding{{App: "fixture", DistDir: measured.DistDir, BaseURL: measured.BaseURL, ArtifactSHA256: manifest.FixturesSHA256}}}
	return opts, manifest
}
func testCollect(ctx context.Context, opts CollectOptions) (*Report, error) {
	return collect(ctx, opts, assetmeasure.CompressorPin{}, func(ctx context.Context, options MeasureOptions) (AppReport, error) {
		return measureApp(ctx, options, testBodyNormalizer)
	}, testBodyNormalizer)
}

func TestCollectUsesLoadedCanonicalCompressorPins(t *testing.T) {
	// A normal executable retains the module metadata used by the pin check.
	dir := t.TempDir()
	source := filepath.Join(dir, "collect.go")
	binary := filepath.Join(dir, "collect")
	program := `package main
import (
 "context"
 "fmt"
 "os"
 "m31labs.dev/gosx/perf/budget"
)
func main() {
 inputs, err := budget.LoadInputs(os.Args[1], budget.LoadOptions{RootDir:os.Args[2]})
 if err != nil { fmt.Fprintln(os.Stderr,err); os.Exit(2) }
 opts := budget.CollectOptions{Inputs:inputs,SHA:os.Args[5],ChunksOnly:os.Args[7]=="true",
 Bindings:[]budget.CollectionBinding{{App:"fixture",DistDir:os.Args[3],BaseURL:os.Args[4],ArtifactSHA256:os.Args[6]}}}
 report,err := budget.Collect(context.Background(),opts)
 if err != nil { fmt.Fprintln(os.Stderr,err); os.Exit(2) }
 if !report.Info.Canonical || len(report.Assets)!=2 { os.Exit(1) }
 for _,a:=range report.Assets { if a.Raw<=0 || a.Gzip<=0 || a.Brotli<=0 { os.Exit(1) } }
}`
	if err := os.WriteFile(source, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", binary, source)
	build.Dir = projectRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("collector executable build failed: %v: %s", err, out)
	}
	for _, chunks := range []bool{false, true} {
		opts, _ := testCollection(t)
		cmd := exec.Command(binary, filepath.Join(projectRoot(t), "perf/budget/testdata/budget.v2.json"), projectRoot(t), opts.Bindings[0].DistDir, opts.Bindings[0].BaseURL, opts.SHA, opts.Bindings[0].ArtifactSHA256, strconv.FormatBool(chunks))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("canonical collection failed: %v: %s", err, out)
		}
	}
}

func TestCollectProductionObservationsAndIndependentProvenance(t *testing.T) {
	opts, manifest := testCollection(t)
	report, err := testCollect(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || report.Mode != "report-only" || len(report.Rows) != 1 || len(report.Assets) != 2 || report.Coverage.RoutesMeasured != 1 || report.Coverage.AssetsMeasured != 2 {
		t.Fatal("observations incorrectly certified or coverage lost")
	}
	if report.Info.ArtifactSHA256 == nil || *report.Info.ArtifactSHA256 != manifest.FixturesSHA256 || report.Info.FixtureSHA256 != opts.Inputs.File.Fixtures.SHA256 || report.Info.SHA != opts.SHA || report.Info.ProfileSHA256 != opts.Inputs.File.Profile.SHA256 || report.Info.CoefficientSHA256 != opts.Inputs.File.Coefficients.SHA256 || !report.Info.Headless || !report.Info.Muted {
		t.Fatal("collector did not bind provenance to loaded inputs and producer")
	}
	if report.Rows[0].ModelStatus != "unknown" || report.Rows[0].NormalizedBytes <= 0 || report.Rows[0].WireBytes <= 0 {
		t.Fatal("collection fabricated a model or failed to measure bodies")
	}
	data, _ := json.Marshal(report)
	if strings.Contains(string(data), opts.Bindings[0].DistDir) || strings.Contains(string(data), opts.Bindings[0].BaseURL) {
		t.Fatal("private locations entered report")
	}
}

func TestCollectRejectsMissingDuplicateAndUnregisteredBindings(t *testing.T) {
	for _, change := range []func(*CollectOptions){
		func(o *CollectOptions) { o.Inputs = nil },
		func(o *CollectOptions) { o.SHA = "private-value" },
		func(o *CollectOptions) { o.Bindings = nil },
		func(o *CollectOptions) { o.Bindings = append(o.Bindings, o.Bindings[0]) },
		func(o *CollectOptions) { o.Bindings[0].App = "unregistered-app" },
		func(o *CollectOptions) { o.Bindings[0].BaseURL = "" },
		func(o *CollectOptions) { o.Bindings[0].DistDir = "" },
		func(o *CollectOptions) { o.Bindings[0].ArtifactSHA256 = "" },
		func(o *CollectOptions) { o.Bindings[0].ArtifactSHA256 = strings.Repeat("f", 64) },
	} {
		opts, _ := testCollection(t)
		change(&opts)
		_, err := testCollect(context.Background(), opts)
		var input *InputError
		if !errors.As(err, &input) || strings.Contains(err.Error(), "private-value") || strings.Contains(err.Error(), "unregistered-app") {
			t.Fatalf("invalid input accepted or echoed: %v", err)
		}
	}
}

func TestCollectRejectsStaleSourceCatalogAndManifestDigests(t *testing.T) {
	for _, change := range []func(*FixtureManifest){
		func(m *FixtureManifest) { m.SourceSHA = strings.Repeat("b", 40) },
		func(m *FixtureManifest) { m.CatalogSHA256 = strings.Repeat("b", 64) },
		func(m *FixtureManifest) { m.Routes[0].InputSequenceID = "changed-input" },
	} {
		opts, manifest := testCollection(t)
		change(manifest)
		writeTestFixtureManifest(t, opts.Bindings[0].DistDir, manifest)
		if _, err := testCollect(context.Background(), opts); err == nil {
			t.Fatal("modified manifest substituted its own producer proof")
		}
	}
	opts, _ := testCollection(t)
	if err := os.WriteFile(filepath.Join(opts.Bindings[0].DistDir, "perf-fixtures.v1.json"), []byte(`{"private-key":"private-value"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := testCollect(context.Background(), opts); err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatal("invalid producer contract accepted or echoed")
	}
}

func TestCollectChunksCannotCertifyRoutesOrExcludeDormantBytes(t *testing.T) {
	opts, _ := testCollection(t)
	opts.ChunksOnly, opts.Bindings[0].BaseURL = true, ""
	report, err := testCollect(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "chunks-only" || report.Passed || len(report.Rows) != 0 || report.Coverage.RoutesMeasured != 0 || report.Coverage.RoutesExpected != 1 || report.Coverage.Reachability != "unknown" || len(report.Assets) != 2 || len(report.Violations) != 1 {
		t.Fatal("offline inventory certified route reachability")
	}
	for _, asset := range report.Assets {
		if asset.Phase != "startup" || asset.Raw <= 0 || asset.Brotli <= 0 {
			t.Fatal("potential bytes were excluded without served closure evidence")
		}
	}
	if err := os.WriteFile(filepath.Join(opts.Bindings[0].DistDir, "counter/index.html"), []byte("modified body"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := testCollect(context.Background(), opts); err == nil {
		t.Fatal("chunks inventory trusted stale declared body hashes")
	}
}

func TestCollectBaseSnapshotAndCancellation(t *testing.T) {
	opts, _ := testCollection(t)
	base := publicTestReport(t)
	digest, epoch := strings.Repeat("9", 64), strings.Repeat("8", 64)
	base.Info.ArtifactSHA256, base.Info.EpochSHA256 = &digest, &epoch
	opts.Base = base
	report, err := testCollect(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Info.BaseSHA != base.Info.SHA || report.Info.BaseArtifactSHA256 != digest || report.Info.EpochSHA256 == nil || *report.Info.EpochSHA256 != epoch {
		t.Fatal("base identity or epoch lost")
	}
	epoch = strings.Repeat("7", 64)
	if *report.Info.EpochSHA256 == epoch {
		t.Fatal("collector aliased mutable base input")
	}
	opts.Base.Info.ArtifactSHA256 = nil
	if _, err := testCollect(context.Background(), opts); err == nil {
		t.Fatal("base without producer evidence accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := testCollect(ctx, opts); err == nil {
		t.Fatal("cancelled collection started")
	}
	if _, err := testCollect(nil, opts); err == nil {
		t.Fatal("nil context accepted")
	}
	opts.Inputs.Toolchain.Go = "1.25.0"
	if _, err := Collect(context.Background(), opts); err == nil {
		t.Fatal("noncanonical compressor accepted")
	}
}

func TestCollectSharedFrameworkIdentityAndStableAggregate(t *testing.T) {
	opts, manifest := testCollection(t)
	extra := opts.Bindings[0]
	extra.App = "extra"
	for i := range manifest.Routes {
		manifest.Routes[i].App = "extra"
	}
	manifest.Assets = []buildmanifest.PerfAssetUse{manifest.Assets[1]}
	manifest.Routes[0].CriticalAssetIDs = []string{manifest.Assets[0].ID}
	extra.DistDir = t.TempDir()
	writeTestFixtureManifest(t, extra.DistDir, manifest)
	extra.ArtifactSHA256 = manifest.FixturesSHA256
	rule := opts.Inputs.File.Routes[0]
	rule.App = "extra"
	opts.Inputs.File.Routes = append(opts.Inputs.File.Routes, rule)
	opts.Bindings = append(opts.Bindings, extra)
	measure := func(_ context.Context, o MeasureOptions) (AppReport, error) {
		phase := "dormant"
		if o.App == "fixture" {
			phase = "startup"
		}
		return AppReport{App: o.App, Assets: []AssetReport{{ID: "framework/island-core", App: o.App, Owner: "framework", Kind: "wasm", Condition: "always", Phase: phase, SHA256: strings.Repeat("1", 64), Raw: 10, Gzip: 8, Brotli: 6, Dependencies: []string{}, ChangedSources: []string{}}}, Coverage: ByteCoverage{Reachability: "known"}}, nil
	}
	before, _ := json.Marshal(opts.Bindings)
	one, err := collect(context.Background(), opts, assetmeasure.CompressorPin{}, measure, testBodyNormalizer)
	if err != nil {
		t.Fatal(err)
	}
	opts.Bindings[0], opts.Bindings[1] = opts.Bindings[1], opts.Bindings[0]
	two, err := collect(context.Background(), opts, assetmeasure.CompressorPin{}, measure, testBodyNormalizer)
	if err != nil || !reflect.DeepEqual(one, two) || len(one.Assets) != 1 || one.Assets[0].Phase != "startup" || one.Coverage.AssetsExpected != 1 || *one.Info.ArtifactSHA256 == manifest.FixturesSHA256 {
		t.Fatalf("shared identity or deterministic aggregate failed: %v", err)
	}
	opts.Bindings[0], opts.Bindings[1] = opts.Bindings[1], opts.Bindings[0]
	after, _ := json.Marshal(opts.Bindings)
	if string(before) != string(after) {
		t.Fatal("binding input mutated")
	}
	bad := func(ctx context.Context, o MeasureOptions) (AppReport, error) {
		r, err := measure(ctx, o)
		if o.App == "fixture" {
			r.Assets[0].Brotli++
		}
		return r, err
	}
	if _, err := collect(context.Background(), opts, assetmeasure.CompressorPin{}, bad, testBodyNormalizer); err == nil {
		t.Fatal("conflicting shared framework identity accepted")
	}
	data, _ := json.Marshal(opts)
	if string(data) != "{}" {
		t.Fatal("native collection options are serializable")
	}
}
