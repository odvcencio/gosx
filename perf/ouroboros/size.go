package ouroboros

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/buildmanifest"
)

const portableCombinedSizeInputLabelsNote = "primary/... and r10/... path fields are portable source identity labels, not artifact-bundle file references; buildInput manifest/export hashes bind the measured files."

type SizeEvidence struct {
	SchemaVersion   string               `json:"schemaVersion"`
	Contract        string               `json:"contractVersion"`
	GeneratedAt     string               `json:"generatedAt,omitempty"`
	Source          SourceIdentity       `json:"source"`
	BuildInput      BuildInputEvidence   `json:"buildInput"`
	ManifestPath    string               `json:"manifestPath"`
	DistDir         string               `json:"distDir"`
	ExportPath      string               `json:"exportPath,omitempty"`
	R10BuildInput   *BuildInputEvidence  `json:"r10BuildInput,omitempty"`
	R10ManifestPath string               `json:"r10ManifestPath,omitempty"`
	R10DistDir      string               `json:"r10DistDir,omitempty"`
	R10ExportPath   string               `json:"r10ExportPath,omitempty"`
	Assets          []TransferredAsset   `json:"assets"`
	Routes          []RouteAssetEvidence `json:"routes"`
	Unresolved      []UnresolvedAssetRef `json:"unresolvedRefs,omitempty"`
	Totals          SizeEvidenceTotals   `json:"totals"`
	Canonical       bool                 `json:"canonical"`
	Notes           []string             `json:"notes,omitempty"`
}

type TransferredAsset struct {
	ID             string   `json:"id"`
	URL            string   `json:"url"`
	Bucket         string   `json:"bucket"`
	File           string   `json:"file"`
	Role           string   `json:"role"`
	SourcePath     string   `json:"sourcePath"`
	ManifestHash   string   `json:"manifestHash,omitempty"`
	SHA256         string   `json:"sha256"`
	Bytes          int64    `json:"bytes"`
	GzipBytes      int64    `json:"gzipBytes"`
	BrotliBytes    int64    `json:"brotliBytes"`
	DuplicateOf    string   `json:"duplicateOf,omitempty"`
	UsedByRoutes   []string `json:"usedByRoutes,omitempty"`
	PlannedVariant string   `json:"plannedVariant,omitempty"`
}

type UnresolvedAssetRef struct {
	Ref    string `json:"ref"`
	Route  string `json:"route,omitempty"`
	Reason string `json:"reason"`
}

type RouteAssetEvidence struct {
	ID                 string              `json:"id,omitempty"`
	Route              string              `json:"route"`
	File               string              `json:"file,omitempty"`
	Capabilities       *ExportCapabilities `json:"capabilities,omitempty"`
	AssetIDs           []string            `json:"assetIds"`
	RawBytes           int64               `json:"rawBytes"`
	GzipBytes          int64               `json:"gzipBytes"`
	BrotliBytes        int64               `json:"brotliBytes"`
	SharedRawBytes     int64               `json:"sharedRawBytes"`
	SharedGzipBytes    int64               `json:"sharedGzipBytes"`
	SharedBrotliBytes  int64               `json:"sharedBrotliBytes"`
	UniqueRawBytes     int64               `json:"uniqueRawBytes"`
	UniqueGzipBytes    int64               `json:"uniqueGzipBytes"`
	UniqueBrotliBytes  int64               `json:"uniqueBrotliBytes"`
	AttributionComment string              `json:"attributionComment,omitempty"`
}

type SizeEvidenceTotals struct {
	AssetCount             int   `json:"assetCount"`
	DistinctContentCount   int   `json:"distinctContentCount"`
	RawBytes               int64 `json:"rawBytes"`
	GzipBytes              int64 `json:"gzipBytes"`
	BrotliBytes            int64 `json:"brotliBytes"`
	DistinctRawBytes       int64 `json:"distinctRawBytes"`
	DistinctGzipBytes      int64 `json:"distinctGzipBytes"`
	DistinctBrotliBytes    int64 `json:"distinctBrotliBytes"`
	RouteCount             int   `json:"routeCount"`
	RoutesWithExplicitRefs int   `json:"routesWithExplicitRefs"`
}

type RuntimeBuildEvidence struct {
	SchemaVersion string                   `json:"schemaVersion"`
	Contract      string                   `json:"contractVersion"`
	GeneratedAt   string                   `json:"generatedAt,omitempty"`
	Source        SourceIdentity           `json:"source"`
	BuildInput    BuildInputEvidence       `json:"buildInput"`
	OutputDir     string                   `json:"outputDir"`
	GoVersion     ToolStatus               `json:"goVersion"`
	TinyGo        ToolStatus               `json:"tinygo"`
	WasmOpt       ToolStatus               `json:"wasmOpt"`
	Variants      []RuntimeArtifactVariant `json:"variants"`
	Notes         []string                 `json:"notes,omitempty"`
}

type ToolStatus struct {
	Name        string            `json:"name"`
	Path        string            `json:"path,omitempty"`
	Version     string            `json:"version,omitempty"`
	GOROOT      string            `json:"goroot,omitempty"`
	TinyGoRoot  string            `json:"tinygoRoot,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Available   bool              `json:"available"`
	Error       string            `json:"error,omitempty"`
}

type RuntimeArtifactVariant struct {
	ID                string        `json:"id"`
	Variant           string        `json:"variant"`
	FeatureMask       uint32        `json:"featureMask"`
	Generation        string        `json:"generation,omitempty"`
	Status            string        `json:"status"`
	SizeBytes         *int64        `json:"sizeBytes"`
	BudgetBytes       *int64        `json:"budgetBytes"`
	File              string        `json:"file,omitempty"`
	SourcePath        string        `json:"sourcePath,omitempty"`
	SHA256            string        `json:"sha256,omitempty"`
	Bytes             int64         `json:"bytes,omitempty"`
	GzipBytes         int64         `json:"gzipBytes,omitempty"`
	BrotliBytes       int64         `json:"brotliBytes,omitempty"`
	BuildArgs         []string      `json:"buildArgs,omitempty"`
	BuildTags         []string      `json:"buildTags,omitempty"`
	TinyGoVersion     string        `json:"tinygoVersion,omitempty"`
	GoVersion         string        `json:"goVersion,omitempty"`
	WasmOptVersion    string        `json:"wasmOptVersion,omitempty"`
	WasmOptAvailable  bool          `json:"wasmOptAvailable"`
	Optimized         bool          `json:"optimized"`
	Shim              *AssetMetrics `json:"shim,omitempty"`
	PlannedSelectedBy []string      `json:"selectedByRoutes"`
	MissingReason     string        `json:"missingReason,omitempty"`
}

type BuildInputEvidence struct {
	GoSXModuleDir              string `json:"gosxModuleDir"`
	GoSXModuleVersion          string `json:"gosxModuleVersion,omitempty"`
	GoWork                     string `json:"goWork,omitempty"`
	GoWorkSHA256               string `json:"goWorkSha256,omitempty"`
	GoModSHA256                string `json:"goModSha256,omitempty"`
	GoSumSHA256                string `json:"goSumSha256,omitempty"`
	ManifestSHA256             string `json:"manifestSha256,omitempty"`
	ExportSHA256               string `json:"exportSha256,omitempty"`
	RejectsModuleCacheMismatch bool   `json:"rejectsModuleCacheMismatch"`
}

type AssetMetrics struct {
	File        string `json:"file"`
	SourcePath  string `json:"sourcePath"`
	SHA256      string `json:"sha256"`
	Bytes       int64  `json:"bytes"`
	GzipBytes   int64  `json:"gzipBytes"`
	BrotliBytes int64  `json:"brotliBytes"`
}

func BuildSizeEvidence(manifestPath, distDir, generatedAt string) (*SizeEvidence, error) {
	return BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath: manifestPath,
		DistDir:      distDir,
		GeneratedAt:  generatedAt,
		RepoRoot:     ".",
		Canonical:    true,
	})
}

type SizeEvidenceOptions struct {
	ManifestPath    string
	DistDir         string
	GeneratedAt     string
	RepoRoot        string
	ArtifactRoot    string
	InventoryPath   string
	R10ManifestPath string
	R10DistDir      string
	Canonical       bool
}

func BuildSizeEvidenceWithOptions(opts SizeEvidenceOptions) (*SizeEvidence, error) {
	repoRoot, err := resolveRepoRootForEvidence(opts.RepoRoot)
	if err != nil {
		return nil, err
	}
	if opts.Canonical {
		if strings.TrimSpace(opts.InventoryPath) == "" {
			return nil, fmt.Errorf("canonical size evidence requires --inventory")
		}
		if strings.TrimSpace(opts.ArtifactRoot) == "" {
			return nil, fmt.Errorf("canonical size evidence requires --ouroboros-out")
		}
	}
	combinedR10 := strings.TrimSpace(opts.R10ManifestPath) != "" || strings.TrimSpace(opts.R10DistDir) != ""
	if combinedR10 && (!opts.Canonical || strings.TrimSpace(opts.R10ManifestPath) == "" || strings.TrimSpace(opts.R10DistDir) == "") {
		return nil, fmt.Errorf("--canonical-r10 requires canonical mode plus an R10 manifest and dist directory")
	}
	primaryExpected := canonicalOuroborosRoutePaths()
	if combinedR10 {
		primaryExpected = canonicalPrimaryOuroborosRoutePaths()
	}
	primary, err := prepareSizeEvidenceSource(opts.ManifestPath, opts.DistDir, opts.Canonical, primaryExpected)
	if err != nil {
		return nil, err
	}
	var r10 *sizeEvidenceBuildSource
	if combinedR10 {
		prepared, err := prepareCanonicalR10SizeEvidenceSource(opts.R10ManifestPath, opts.R10DistDir)
		if err != nil {
			return nil, fmt.Errorf("canonical R10 source: %w", err)
		}
		r10 = &prepared
	}
	artifactRoot := opts.ArtifactRoot
	if strings.TrimSpace(artifactRoot) == "" {
		artifactRoot, err = os.MkdirTemp("", "gosx-ouroboros-source-*")
		if err != nil {
			return nil, err
		}
	}
	if opts.Canonical {
		if err := validateExportHTMLAttribution(primary.distDir, primary.export); err != nil {
			return nil, err
		}
		if r10 != nil {
			if err := validateExportHTMLAttribution(r10.distDir, r10.export); err != nil {
				return nil, fmt.Errorf("canonical R10 source: %w", err)
			}
		}
	}
	inventoryPath := opts.InventoryPath
	if opts.Canonical {
		materializedPath, err := MaterializeCanonicalInventory(contextBackground(), repoRoot, opts.InventoryPath, artifactRoot)
		if err != nil {
			return nil, err
		}
		inventoryPath = materializedPath
	}
	source, err := browserSourceIdentity(contextBackground(), BrowserBaselineOptions{
		RepoRoot:      repoRoot,
		InventoryPath: inventoryPath,
		ArtifactRoot:  artifactRoot,
	})
	if err != nil {
		return nil, err
	}
	if opts.Canonical {
		if err := requireCanonicalSourceIdentity(source); err != nil {
			return nil, err
		}
	}
	buildInput, err := collectBuildInputEvidence(repoRoot, primary.manifestPath, primary.export.path)
	if err != nil {
		return nil, err
	}
	if opts.Canonical {
		buildInput = portableSizeBuildInput(buildInput)
	}
	report := &SizeEvidence{
		SchemaVersion: SchemaVersion,
		Contract:      ContractO02,
		GeneratedAt:   opts.GeneratedAt,
		Source:        source,
		BuildInput:    buildInput,
		ManifestPath:  primary.manifestPath,
		DistDir:       primary.distDir,
		Canonical:     opts.Canonical,
	}
	if primary.export.path != "" {
		report.ExportPath = primary.export.path
	}
	if opts.Canonical {
		report.ManifestPath = "primary/build.json"
		report.DistDir = "primary"
		report.ExportPath = "primary/export.json"
	}
	if err := appendSizeEvidenceSource(report, primary, opts.Canonical, portableSizeSourceLabel(opts.Canonical, "primary")); err != nil {
		return report, err
	}
	if r10 != nil {
		r10BuildInput, err := collectBuildInputEvidence(repoRoot, r10.manifestPath, r10.export.path)
		if err != nil {
			return nil, err
		}
		r10BuildInput = portableSizeBuildInput(r10BuildInput)
		report.R10BuildInput = &r10BuildInput
		report.R10ManifestPath = "r10/build.json"
		report.R10DistDir = "r10"
		report.R10ExportPath = "r10/export.json"
		report.Notes = append(report.Notes, portableCombinedSizeInputLabelsNote)
		report.Notes = append(report.Notes, "R10 size evidence was combined from the external examples/gosx-docs export; no corpus-local route was fabricated.")
		if err := appendSizeEvidenceSource(report, *r10, true, "r10"); err != nil {
			return report, err
		}
	}
	fillSizeEvidenceTotals(report)
	if len(report.Routes) == 0 {
		report.Notes = append(report.Notes, "No export.json was present; assets are shared build outputs without per-route attribution.")
	}
	if len(report.Unresolved) > 0 {
		report.Canonical = false
		if opts.Canonical {
			return report, fmt.Errorf("unresolved route asset refs: %d", len(report.Unresolved))
		}
	}
	return report, nil
}

type sizeEvidenceBuildSource struct {
	manifestPath string
	distDir      string
	manifest     *buildmanifest.Manifest
	export       exportEvidence
}

func prepareSizeEvidenceSource(manifestPath, distDir string, canonical bool, expectedRoutes map[string]bool) (sizeEvidenceBuildSource, error) {
	manifest, err := buildmanifest.Load(manifestPath)
	if err != nil {
		return sizeEvidenceBuildSource{}, err
	}
	absDist, err := filepath.Abs(distDir)
	if err != nil {
		return sizeEvidenceBuildSource{}, err
	}
	exportManifest, err := loadExportEvidenceForRoutes(filepath.Join(absDist, "export.json"), canonical, expectedRoutes)
	if err != nil {
		return sizeEvidenceBuildSource{}, err
	}
	return sizeEvidenceBuildSource{manifestPath: manifestPath, distDir: absDist, manifest: manifest, export: exportManifest}, nil
}

func prepareCanonicalR10SizeEvidenceSource(manifestPath, distDir string) (sizeEvidenceBuildSource, error) {
	manifest, err := buildmanifest.Load(manifestPath)
	if err != nil {
		return sizeEvidenceBuildSource{}, err
	}
	absDist, err := filepath.Abs(distDir)
	if err != nil {
		return sizeEvidenceBuildSource{}, err
	}
	exportManifest, err := loadCanonicalR10ExportEvidence(filepath.Join(absDist, "export.json"))
	if err != nil {
		return sizeEvidenceBuildSource{}, err
	}
	return sizeEvidenceBuildSource{manifestPath: manifestPath, distDir: absDist, manifest: manifest, export: exportManifest}, nil
}

func appendSizeEvidenceSource(report *SizeEvidence, source sizeEvidenceBuildSource, canonical bool, portableLabel string) error {
	refs := map[string]string{}
	if portableLabel == "r10" {
		// The docs export is a full site. Only transfers reachable from the
		// selected R10 HTML belong to this combined evidence source.
		for _, route := range source.export.routes {
			htmlPath, err := routeHTMLPath(source.distDir, route.File)
			if err != nil {
				return fmt.Errorf("route %s HTML: %w", route.Path, err)
			}
			for _, ref := range refsFromHTMLFile(htmlPath) {
				refs[ref] = "route-transfer"
			}
		}
	} else {
		addManifestRuntimeRefs(refs, source.manifest)
		for _, ref := range source.export.assetRefs {
			refs[ref] = "route-transfer"
		}
	}
	assets, unresolved, err := collectTransferredAssetsForSource(source.distDir, source.manifest, refs, canonical, portableLabel)
	if err != nil {
		return err
	}
	mergeTransferredAssets(report, assets)
	report.Unresolved = append(report.Unresolved, unresolved...)
	return attributeRoutesFromSource(report, source.distDir, source.export, source.manifest, canonical, portableLabel)
}

func portableSizeSourceLabel(canonical bool, label string) string {
	if canonical {
		return label
	}
	return ""
}

func portableSizeBuildInput(input BuildInputEvidence) BuildInputEvidence {
	input.GoSXModuleDir = "."
	input.GoWork = ""
	return input
}

func EnsureNewJSONFilePath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("missing evidence path")
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("refusing to overwrite existing evidence %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func BuildCanonicalSourceIdentity(ctx context.Context, repoRoot, inventoryPath, artifactRoot string) (SourceIdentity, error) {
	if strings.TrimSpace(inventoryPath) == "" {
		return SourceIdentity{}, fmt.Errorf("canonical source identity requires --inventory")
	}
	if strings.TrimSpace(artifactRoot) == "" {
		return SourceIdentity{}, fmt.Errorf("canonical source identity requires --ouroboros-out")
	}
	root, err := resolveRepoRootForEvidence(repoRoot)
	if err != nil {
		return SourceIdentity{}, err
	}
	materializedPath, err := MaterializeCanonicalInventory(ctx, root, inventoryPath, artifactRoot)
	if err != nil {
		return SourceIdentity{}, err
	}
	source, err := BuildSourceIdentity(ctx, root, materializedPath, artifactRoot)
	if err != nil {
		return SourceIdentity{}, err
	}
	if err := requireCanonicalSourceIdentity(source); err != nil {
		return SourceIdentity{}, err
	}
	return source, nil
}

func requireCanonicalSourceIdentity(source SourceIdentity) error {
	if !source.StrictInventory {
		return fmt.Errorf("canonical evidence requires strict inventory")
	}
	if !source.CurrentOverlayVerified {
		return fmt.Errorf("canonical evidence requires fresh source inventory")
	}
	if !source.ReconstructionProof {
		return fmt.Errorf("canonical evidence requires verified reconstruction proof")
	}
	if source.InventorySHA256 == "" || source.InventoryRef == "" {
		return fmt.Errorf("canonical evidence requires inventory identity")
	}
	return nil
}

func MaterializeCanonicalInventory(ctx context.Context, repoRoot, inventoryPath, artifactRoot string) (string, error) {
	root, err := resolveRepoRootForEvidence(repoRoot)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(inventoryPath) == "" {
		return "", fmt.Errorf("canonical inventory materialization requires --inventory")
	}
	if strings.TrimSpace(artifactRoot) == "" {
		return "", fmt.Errorf("canonical inventory materialization requires artifact root")
	}
	f, err := os.Open(inventoryPath)
	if err != nil {
		return "", err
	}
	inv, err := DecodeInventoryStrict(f)
	_ = f.Close()
	if err != nil {
		return "", err
	}
	if err := ValidateInventoryFresh(ctx, root, inv); err != nil {
		return "", err
	}
	if err := requireInventoryReplay(ctx, root, inv); err != nil {
		return "", err
	}
	artifactRootAbs, err := filepath.Abs(artifactRoot)
	if err != nil {
		return "", err
	}
	sourceDir := filepath.Join(artifactRootAbs, "source")
	materializedPath := filepath.Join(sourceDir, "source-inventory.json")
	candidate, err := cloneInventory(inv)
	if err != nil {
		return "", err
	}
	normalizePortableInventoryMetadata(candidate)
	rewriteMaterializedOverlayRefs(root, sourceDir, candidate)
	if _, err := os.Lstat(materializedPath); err == nil {
		return materializedPath, validateReusableMaterializedInventory(ctx, root, materializedPath, candidate)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := materializeOverlayInputs(root, sourceDir, inv); err != nil {
		return "", err
	}
	normalizePortableInventoryMetadata(inv)
	rewriteMaterializedOverlayRefs(root, sourceDir, inv)
	if err := WriteNewJSONFile(materializedPath, inv); err != nil {
		return "", err
	}
	return materializedPath, nil
}

func normalizePortableInventoryMetadata(inv *Inventory) {
	if inv == nil {
		return
	}
	inv.ArtifactRoot = portableArtifactRoot
	inv.Manifest.ArtifactRoot = portableArtifactRoot
}

func validateReusableMaterializedInventory(ctx context.Context, repoRoot, materializedPath string, candidate *Inventory) error {
	f, err := os.Open(materializedPath)
	if err != nil {
		return err
	}
	existing, err := DecodeInventoryStrict(f)
	_ = f.Close()
	if err != nil {
		return err
	}
	if err := ValidateInventoryFresh(ctx, repoRoot, existing); err != nil {
		return err
	}
	if err := requireInventoryReplay(ctx, repoRoot, existing); err != nil {
		return err
	}
	got, err := canonicalInventoryJSON(existing)
	if err != nil {
		return err
	}
	want, err := canonicalInventoryJSON(candidate)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("contained canonical inventory differs from requested inventory")
	}
	return nil
}

func requireInventoryReplay(ctx context.Context, repoRoot string, inv *Inventory) error {
	reconstruction, err := ReplayInventoryReconstruction(ctx, repoRoot, inv)
	if err != nil {
		return fmt.Errorf("reconstruction proof: %w", err)
	}
	if !reconstruction.Isolated || !reconstruction.Applied || !reconstruction.Verified {
		return fmt.Errorf("reconstruction proof incomplete")
	}
	return nil
}

func cloneInventory(inv *Inventory) (*Inventory, error) {
	body, err := json.Marshal(inv)
	if err != nil {
		return nil, err
	}
	var out Inventory
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func canonicalInventoryJSON(inv *Inventory) ([]byte, error) {
	body, err := json.Marshal(inv)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := json.Unmarshal(body, &normalized); err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func rewriteMaterializedOverlayRefs(repoRoot, sourceDir string, inv *Inventory) {
	if inv == nil {
		return
	}
	if inv.Overlay.PatchPath != "" {
		inv.Overlay.PatchPath = relTo(repoRoot, filepath.Join(sourceDir, "tracked-overlay.patch"))
	}
	if len(inv.Overlay.UntrackedSources) > 0 {
		inv.Overlay.ArchivePath = relTo(repoRoot, filepath.Join(sourceDir, "untracked-sources"))
	}
}

func materializeOverlayInputs(repoRoot, sourceDir string, inv *Inventory) error {
	if inv == nil {
		return fmt.Errorf("missing inventory")
	}
	if inv.Overlay.PatchPath != "" {
		src, err := containedReconstructionEvidencePath(repoRoot, inv.Overlay.PatchPath)
		if err != nil {
			return err
		}
		dst := filepath.Join(sourceDir, "tracked-overlay.patch")
		if err := ensureContainedRepoDestination(repoRoot, dst); err != nil {
			return err
		}
		if err := CopyFile(dst, src); err != nil {
			return err
		}
		inv.Overlay.PatchPath = relTo(repoRoot, dst)
	}
	if len(inv.Overlay.UntrackedSources) > 0 {
		if inv.Overlay.ArchivePath == "" {
			return fmt.Errorf("canonical inventory has untracked sources but no archivePath")
		}
		src, err := containedReconstructionEvidencePath(repoRoot, inv.Overlay.ArchivePath)
		if err != nil {
			return err
		}
		dst := filepath.Join(sourceDir, "untracked-sources")
		if err := ensureContainedRepoDestination(repoRoot, dst); err != nil {
			return err
		}
		if err := copySourceEvidenceTree(dst, src); err != nil {
			return err
		}
		inv.Overlay.ArchivePath = relTo(repoRoot, dst)
	}
	return nil
}

func ensureContainedRepoDestination(repoRoot, dst string) error {
	rootAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return err
	}
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, dstAbs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("canonical artifact root must be inside repo root for replay: %s", dst)
	}
	return nil
}

func copySourceEvidenceTree(dstRoot, srcRoot string) error {
	return filepath.WalkDir(srcRoot, func(src string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, src)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dstRoot, 0o755)
		}
		dst, err := safeJoin(dstRoot, rel)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		if mode&os.ModeSymlink != 0 {
			target, err := os.Readlink(src)
			if err != nil {
				return err
			}
			if err := validateSafeSymlinkTarget(target); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			return os.Symlink(target, dst)
		}
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if !mode.IsRegular() {
			return fmt.Errorf("unsupported overlay archive member %s", src)
		}
		if err := CopyFile(dst, src); err != nil {
			return err
		}
		return os.Chmod(dst, mode.Perm())
	})
}

func resolveRepoRootForEvidence(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	out := commandOutput(root, "go", "list", "-m", "-f", "{{.Dir}}", "m31labs.dev/gosx")
	if out != "" {
		return out, nil
	}
	return filepath.Abs(root)
}

func WriteJSONFile(path string, value any) error {
	return writeJSONFileAtomic(path, value, true)
}

func WriteNewJSONFile(path string, value any) error {
	return writeJSONFileAtomic(path, value, false)
}

func writeJSONFileAtomic(path string, value any, overwrite bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if !overwrite {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("refusing to overwrite existing evidence %s", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if !overwrite {
		if err := os.Link(tmpPath, path); err != nil {
			return err
		}
		cleanup = false
		_ = os.Remove(tmpPath)
		return nil
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func MetricsForFile(file string) (AssetMetrics, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return AssetMetrics{}, err
	}
	sum := sha256.Sum256(data)
	return AssetMetrics{
		File:        filepath.Base(file),
		SourcePath:  file,
		SHA256:      hex.EncodeToString(sum[:]),
		Bytes:       int64(len(data)),
		GzipBytes:   GzipLength(data),
		BrotliBytes: BrotliLength(data),
	}, nil
}

func GzipLength(data []byte) int64 {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return int64(buf.Len())
}

func BrotliLength(data []byte) int64 {
	var buf bytes.Buffer
	zw := brotli.NewWriterLevel(&buf, brotli.BestCompression)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return int64(buf.Len())
}

type exportEvidence struct {
	path      string
	routes    []exportEvidenceRoute
	assetRefs []string
}

type exportEvidenceRoute struct {
	Path         string              `json:"path"`
	File         string              `json:"file"`
	Capabilities *ExportCapabilities `json:"capabilities"`
}

// ExportCapabilities is the measured enhancement surface written by
// export.json. A pointer is intentional: a missing measurement must not be
// confused with a route that was measured and found to have zero client
// runtime requirements.
type ExportCapabilities struct {
	Navigation    bool   `json:"navigation"`
	Bootstrap     bool   `json:"bootstrap"`
	BootstrapMode string `json:"bootstrapMode,omitempty"`
	WASM          bool   `json:"wasm"`
	Islands       int    `json:"islands,omitempty"`
	Engines       int    `json:"engines,omitempty"`
	Hubs          int    `json:"hubs,omitempty"`
	Scene3D       bool   `json:"scene3d,omitempty"`
	Video         bool   `json:"video,omitempty"`
	Motion        bool   `json:"motion,omitempty"`
}

func (c *ExportCapabilities) measuredZero() bool {
	return c != nil &&
		!c.Navigation &&
		!c.Bootstrap &&
		strings.TrimSpace(c.BootstrapMode) == "" &&
		!c.WASM &&
		c.Islands == 0 &&
		c.Engines == 0 &&
		c.Hubs == 0 &&
		!c.Scene3D &&
		!c.Video &&
		!c.Motion
}

func loadExportEvidence(exportPath string, canonical bool) (exportEvidence, error) {
	return loadExportEvidenceForRoutes(exportPath, canonical, canonicalOuroborosRoutePaths())
}

func loadExportEvidenceForRoutes(exportPath string, canonical bool, expectedRoutes map[string]bool) (exportEvidence, error) {
	data, err := os.ReadFile(exportPath)
	if err != nil {
		if canonical {
			return exportEvidence{}, fmt.Errorf("canonical size evidence requires export.json: %w", err)
		}
		return exportEvidence{}, nil
	}
	var raw struct {
		Routes    []exportEvidenceRoute `json:"routes"`
		AssetRefs []string              `json:"assetRefs"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		if canonical {
			return exportEvidence{}, fmt.Errorf("decode export.json: %w", err)
		}
		return exportEvidence{}, nil
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if canonical {
			return exportEvidence{}, fmt.Errorf("decode export.json: trailing JSON")
		}
		return exportEvidence{}, nil
	}
	if canonical && len(raw.Routes) == 0 {
		return exportEvidence{}, fmt.Errorf("canonical size evidence requires routes in export.json")
	}
	out := exportEvidence{path: exportPath, routes: raw.Routes}
	seenRoutes := map[string]bool{}
	for _, ref := range raw.AssetRefs {
		if normalized := normalizeGosxRef(ref); normalized != "" {
			out.assetRefs = append(out.assetRefs, normalized)
		}
	}
	for _, route := range raw.Routes {
		routePath := strings.TrimSpace(route.Path)
		routeFile := strings.TrimSpace(route.File)
		if routePath == "" {
			if canonical {
				return exportEvidence{}, fmt.Errorf("export.json contains an empty route")
			}
			continue
		}
		if seenRoutes[routePath] {
			if canonical {
				return exportEvidence{}, fmt.Errorf("export.json contains duplicate route %s", routePath)
			}
			continue
		}
		seenRoutes[routePath] = true
		if canonical && routeFile == "" {
			return exportEvidence{}, fmt.Errorf("export.json route %s has no HTML file", routePath)
		}
	}
	if canonical {
		if err := validateCanonicalOuroborosRoutes(raw.Routes, expectedRoutes); err != nil {
			return exportEvidence{}, err
		}
	}
	sort.Strings(out.assetRefs)
	return out, nil
}

func loadCanonicalR10ExportEvidence(exportPath string) (exportEvidence, error) {
	data, err := os.ReadFile(exportPath)
	if err != nil {
		return exportEvidence{}, fmt.Errorf("canonical R10 size evidence requires full-site export.json: %w", err)
	}
	var raw struct {
		Routes    []exportEvidenceRoute `json:"routes"`
		AssetRefs []string              `json:"assetRefs"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return exportEvidence{}, fmt.Errorf("decode canonical R10 full-site export.json: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return exportEvidence{}, fmt.Errorf("decode canonical R10 full-site export.json: trailing JSON")
	}
	if len(raw.Routes) == 0 {
		return exportEvidence{}, fmt.Errorf("canonical R10 size evidence requires routes in full-site export.json")
	}
	selected := exportEvidence{path: exportPath, assetRefs: []string{}}
	for _, route := range raw.Routes {
		if strings.TrimSpace(route.Path) != "/demos/water" {
			continue
		}
		if len(selected.routes) != 0 {
			return exportEvidence{}, fmt.Errorf("full-site export.json contains duplicate route /demos/water")
		}
		if strings.TrimSpace(route.File) == "" {
			return exportEvidence{}, fmt.Errorf("full-site export.json route /demos/water has no HTML file")
		}
		if route.Capabilities == nil {
			return exportEvidence{}, fmt.Errorf("full-site export.json route /demos/water has no capability measurement")
		}
		selected.routes = append(selected.routes, route)
	}
	if len(selected.routes) != 1 {
		return exportEvidence{}, fmt.Errorf("full-site export.json is missing canonical route /demos/water")
	}
	return selected, nil
}

func validateCanonicalOuroborosRoutes(routes []exportEvidenceRoute, expected map[string]bool) error {
	if len(expected) == 0 {
		return fmt.Errorf("canonical size evidence has no expected routes")
	}
	seen := map[string]bool{}
	for _, route := range routes {
		path := strings.TrimSpace(route.Path)
		if !expected[path] {
			return fmt.Errorf("export.json route %s is not in canonical Ouroboros corpus", path)
		}
		seen[path] = true
	}
	missing := []string{}
	for path := range expected {
		if !seen[path] {
			missing = append(missing, path)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Errorf("export.json missing canonical Ouroboros routes: %s", strings.Join(missing, ","))
	}
	return nil
}

func canonicalOuroborosRoutePaths() map[string]bool {
	return map[string]bool{
		"/static":          true,
		"/lite":            true,
		"/island/counter":  true,
		"/islands/kitchen": true,
		"/action/form":     true,
		"/canvas-board":    true,
		"/hub/echo":        true,
		"/video-sync":      true,
		"/scene/basic":     true,
		"/navigation/a":    true,
		"/navigation/b":    true,
		"/demos/water":     true,
	}
}

func canonicalPrimaryOuroborosRoutePaths() map[string]bool {
	paths := canonicalOuroborosRoutePaths()
	delete(paths, "/demos/water")
	return paths
}

func canonicalOuroborosRouteID(routePath string) string {
	switch routePath {
	case "/static":
		return "R00"
	case "/lite":
		return "R01"
	case "/island/counter":
		return "R02"
	case "/islands/kitchen":
		return "R03"
	case "/action/form":
		return "R04"
	case "/canvas-board":
		return "R05"
	case "/hub/echo":
		return "R06"
	case "/video-sync":
		return "R07"
	case "/scene/basic":
		return "R08"
	case "/navigation/a":
		return "R09A"
	case "/navigation/b":
		return "R09B"
	case "/demos/water":
		return "R10"
	default:
		return ""
	}
}

func routeAllowsNoRuntimeAttribution(route exportEvidenceRoute) bool {
	switch canonicalOuroborosRouteID(route.Path) {
	case "R00", "R05":
		return route.Capabilities.measuredZero()
	default:
		return false
	}
}

func addManifestRuntimeRefs(refs map[string]string, manifest *buildmanifest.Manifest) {
	if manifest == nil {
		return
	}
	for _, item := range []struct {
		ref  string
		file string
	}{
		{"/gosx/runtime.wasm", manifest.Runtime.WASM.File},
		{"/gosx/runtime-islands.wasm", manifest.Runtime.WASMIslands.File},
		{"/gosx/wasm_exec.js", manifest.Runtime.WASMExec.File},
		{"/gosx/standard-go-wasm_exec.js", manifest.Runtime.StandardGoWASMExec.File},
		{"/gosx/bootstrap.js", manifest.Runtime.Bootstrap.File},
		{"/gosx/bootstrap-lite.js", manifest.Runtime.BootstrapLite.File},
		{"/gosx/bootstrap-runtime.js", manifest.Runtime.BootstrapRuntime.File},
		{"/gosx/bootstrap-feature-islands.js", manifest.Runtime.BootstrapFeatureIslands.File},
		{"/gosx/bootstrap-feature-engines.js", manifest.Runtime.BootstrapFeatureEngines.File},
		{"/gosx/bootstrap-feature-hubs.js", manifest.Runtime.BootstrapFeatureHubs.File},
		{"/gosx/bootstrap-feature-controllers.js", manifest.Runtime.BootstrapFeatureControllers.File},
		{"/gosx/bootstrap-feature-textlayout.js", manifest.Runtime.BootstrapFeatureTextlayout.File},
		{"/gosx/bootstrap-feature-scene3d.js", manifest.Runtime.BootstrapFeatureScene3D.File},
		{"/gosx/bootstrap-feature-scene3d-command.js", manifest.Runtime.BootstrapFeatureScene3DCommand.File},
		{"/gosx/bootstrap-feature-scene3d-webgpu.js", manifest.Runtime.BootstrapFeatureScene3DWebGPU.File},
		{"/gosx/bootstrap-feature-scene3d-webgl.js", manifest.Runtime.BootstrapFeatureScene3DWebGL.File},
		{"/gosx/bootstrap-feature-scene3d-gltf.js", manifest.Runtime.BootstrapFeatureScene3DGLTF.File},
		{"/gosx/bootstrap-feature-scene3d-animation.js", manifest.Runtime.BootstrapFeatureScene3DAnimation.File},
		{"/gosx/bootstrap-feature-scene3d-compute.js", manifest.Runtime.BootstrapFeatureScene3DCompute.File},
		{"/gosx/bootstrap-feature-scene3d-decompress.js", manifest.Runtime.BootstrapFeatureScene3DDecompress.File},
		{"/gosx/patch.js", manifest.Runtime.Patch.File},
		{"/gosx/hls.min.js", manifest.Runtime.VideoHLS.File},
		{"/gosx/stripe-bridge.js", manifest.Runtime.StripeBridge.File},
		{"/gosx/relay.js", manifest.Runtime.Relay.File},
	} {
		if strings.TrimSpace(item.file) != "" {
			refs[item.ref] = "manifest-runtime"
		}
	}
	for _, asset := range manifest.Islands {
		ext := ".gxi"
		if asset.Format == "json" {
			ext = ".json"
		}
		refs["/gosx/islands/"+asset.Name+ext] = "manifest-island"
	}
	for _, asset := range manifest.CSS {
		name := filepath.Base(asset.Source)
		if name == "." || name == "" {
			name = asset.Component + ".css"
		}
		refs["/gosx/css/"+name] = "manifest-css"
	}
}

func collectTransferredAssets(distDir string, manifest *buildmanifest.Manifest, refs map[string]string, canonical bool) ([]TransferredAsset, []UnresolvedAssetRef, error) {
	return collectTransferredAssetsForSource(distDir, manifest, refs, canonical, "")
}

func collectTransferredAssetsForSource(distDir string, manifest *buildmanifest.Manifest, refs map[string]string, canonical bool, portableLabel string) ([]TransferredAsset, []UnresolvedAssetRef, error) {
	keys := make([]string, 0, len(refs))
	for ref := range refs {
		keys = append(keys, ref)
	}
	sort.Strings(keys)
	byHash := map[string]string{}
	out := make([]TransferredAsset, 0, len(keys))
	unresolved := []UnresolvedAssetRef{}
	for _, ref := range keys {
		sourcePath, asset, ok := manifestRefSource(distDir, manifest, ref)
		if !ok {
			unresolved = append(unresolved, UnresolvedAssetRef{Ref: ref, Reason: "not found in build manifest or dist assets"})
			continue
		}
		if canonical && strings.TrimSpace(asset.Hash) == "" {
			unresolved = append(unresolved, UnresolvedAssetRef{Ref: ref, Reason: "canonical asset is not source-bound by build manifest hash"})
			continue
		}
		metrics, err := MetricsForFile(sourcePath)
		if err != nil {
			return nil, nil, fmt.Errorf("measure %s: %w", sourcePath, err)
		}
		entry := TransferredAsset{
			ID:           stableAssetID(ref, metrics.SHA256),
			URL:          ref,
			Bucket:       assetBucket(ref),
			File:         filepath.Base(sourcePath),
			Role:         refs[ref],
			SourcePath:   sizeEvidenceSourcePath(distDir, sourcePath, portableLabel),
			ManifestHash: asset.Hash,
			SHA256:       metrics.SHA256,
			Bytes:        metrics.Bytes,
			GzipBytes:    metrics.GzipBytes,
			BrotliBytes:  metrics.BrotliBytes,
		}
		if prior, ok := byHash[entry.SHA256]; ok {
			entry.DuplicateOf = prior
		} else {
			byHash[entry.SHA256] = entry.ID
		}
		out = append(out, entry)
	}
	return out, unresolved, nil
}

func mergeTransferredAssets(report *SizeEvidence, assets []TransferredAsset) {
	if report == nil {
		return
	}
	seen := make(map[string]bool, len(report.Assets))
	for _, asset := range report.Assets {
		seen[asset.ID] = true
	}
	for _, asset := range assets {
		if seen[asset.ID] {
			continue
		}
		seen[asset.ID] = true
		report.Assets = append(report.Assets, asset)
	}
}

func sizeEvidenceSourcePath(distDir, sourcePath, portableLabel string) string {
	if strings.TrimSpace(portableLabel) == "" {
		return sourcePath
	}
	rel, err := filepath.Rel(distDir, sourcePath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return filepath.ToSlash(filepath.Join(portableLabel, filepath.Base(sourcePath)))
	}
	return filepath.ToSlash(filepath.Join(portableLabel, rel))
}

func manifestRefSource(distDir string, manifest *buildmanifest.Manifest, ref string) (string, buildmanifest.HashedAsset, bool) {
	ref = normalizeGosxRef(ref)
	if ref == "" || manifest == nil {
		return "", buildmanifest.HashedAsset{}, false
	}
	runtimeDir := filepath.Join(distDir, "assets", "runtime")
	if rel, ok := strings.CutPrefix(ref, "/gosx/assets/"); ok && rel != "" {
		return directAssetSource(distDir, manifest, rel)
	}
	switch ref {
	case "/gosx/runtime.wasm":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.WASM)
	case "/gosx/runtime-islands.wasm":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.WASMIslands)
	case "/gosx/wasm_exec.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.WASMExec)
	case "/gosx/standard-go-wasm_exec.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.StandardGoWASMExec)
	case "/gosx/bootstrap.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.Bootstrap)
	case "/gosx/bootstrap-lite.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapLite)
	case "/gosx/bootstrap-runtime.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapRuntime)
	case "/gosx/bootstrap-feature-islands.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapFeatureIslands)
	case "/gosx/bootstrap-feature-engines.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapFeatureEngines)
	case "/gosx/bootstrap-feature-hubs.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapFeatureHubs)
	case "/gosx/bootstrap-feature-controllers.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapFeatureControllers)
	case "/gosx/bootstrap-feature-textlayout.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapFeatureTextlayout)
	case "/gosx/bootstrap-feature-scene3d.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapFeatureScene3D)
	case "/gosx/bootstrap-feature-scene3d-command.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapFeatureScene3DCommand)
	case "/gosx/bootstrap-feature-scene3d-webgpu.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapFeatureScene3DWebGPU)
	case "/gosx/bootstrap-feature-scene3d-webgl.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapFeatureScene3DWebGL)
	case "/gosx/bootstrap-feature-scene3d-gltf.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapFeatureScene3DGLTF)
	case "/gosx/bootstrap-feature-scene3d-animation.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapFeatureScene3DAnimation)
	case "/gosx/bootstrap-feature-scene3d-compute.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapFeatureScene3DCompute)
	case "/gosx/bootstrap-feature-scene3d-decompress.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.BootstrapFeatureScene3DDecompress)
	case "/gosx/patch.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.Patch)
	case "/gosx/hls.min.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.VideoHLS)
	case "/gosx/stripe-bridge.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.StripeBridge)
	case "/gosx/relay.js":
		return runtimeAssetSource(runtimeDir, manifest.Runtime.Relay)
	}
	if rel, ok := strings.CutPrefix(ref, "/gosx/islands/"); ok {
		name := filepath.Base(rel)
		for _, asset := range manifest.Islands {
			ext := ".gxi"
			if asset.Format == "json" {
				ext = ".json"
			}
			if asset.Name+ext == name {
				return filepath.Join(distDir, "assets", "islands", asset.File), asset.HashedAsset, true
			}
		}
	}
	if rel, ok := strings.CutPrefix(ref, "/gosx/css/"); ok {
		name := filepath.Base(rel)
		for _, asset := range manifest.CSS {
			candidate := filepath.Base(asset.Source)
			if candidate == "." || candidate == "" {
				candidate = asset.Component + ".css"
			}
			if candidate == name {
				return filepath.Join(distDir, "assets", "css", asset.File), asset.HashedAsset, true
			}
		}
	}
	return "", buildmanifest.HashedAsset{}, false
}

func runtimeAssetSource(runtimeDir string, asset buildmanifest.HashedAsset) (string, buildmanifest.HashedAsset, bool) {
	if strings.TrimSpace(asset.File) == "" {
		return "", buildmanifest.HashedAsset{}, false
	}
	full, err := containedPath(runtimeDir, asset.File)
	if err != nil {
		return "", buildmanifest.HashedAsset{}, false
	}
	return full, asset, true
}

func normalizeGosxRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if parsed, err := url.Parse(ref); err == nil && parsed != nil {
		ref = parsed.Path
	}
	clean := path.Clean("/" + strings.TrimLeft(ref, "/"))
	if !strings.HasPrefix(clean, "/gosx/") {
		return ""
	}
	return clean
}

func assetBucket(ref string) string {
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) == 2 {
		return "runtime"
	}
	if len(parts) >= 2 {
		return parts[1]
	}
	return "runtime"
}

func stableAssetID(ref, contentSHA256 string) string {
	sum := sha256.Sum256([]byte(ref + "\x00" + strings.TrimSpace(contentSHA256)))
	return "asset-" + hex.EncodeToString(sum[:])[:16]
}

func attributeRoutes(report *SizeEvidence, exportManifest exportEvidence, manifest *buildmanifest.Manifest, canonical bool) error {
	return attributeRoutesFromSource(report, report.DistDir, exportManifest, manifest, canonical, "")
}

func attributeRoutesFromSource(report *SizeEvidence, distDir string, exportManifest exportEvidence, manifest *buildmanifest.Manifest, canonical bool, portableLabel string) error {
	byURL := map[string]int{}
	for i := range report.Assets {
		byURL[report.Assets[i].URL] = i
	}
	for _, route := range exportManifest.routes {
		htmlPath, err := routeHTMLPath(distDir, route.File)
		if err != nil {
			if canonical {
				return fmt.Errorf("route %s HTML: %w", route.Path, err)
			}
			htmlPath = filepath.Join(distDir, filepath.FromSlash(route.File))
		}
		refs := refsFromHTMLFile(htmlPath)
		if canonical && len(refs) == 0 && !routeAllowsNoRuntimeAttribution(route) {
			return fmt.Errorf("route %s has incomplete asset attribution", route.Path)
		}
		entry := RouteAssetEvidence{
			Route:        route.Path,
			File:         route.File,
			Capabilities: route.Capabilities,
		}
		if route.Path != "" {
			entry.ID = "route-" + strings.Trim(strings.ReplaceAll(route.Path, "/", "-"), "-")
			if entry.ID == "route-" {
				entry.ID = "route-root"
			}
		}
		seen := map[string]bool{}
		for _, ref := range refs {
			assetIndex, assetFound := byURL[ref]
			if !assetFound {
				if sourcePath, hashed, ok := manifestRefSource(distDir, manifest, ref); ok {
					if canonical && strings.TrimSpace(hashed.Hash) == "" {
						report.Unresolved = append(report.Unresolved, UnresolvedAssetRef{Ref: ref, Route: route.Path, Reason: "canonical route asset is not source-bound by build manifest hash"})
						continue
					}
					metrics, err := MetricsForFile(sourcePath)
					if err == nil {
						id := stableAssetID(ref, metrics.SHA256)
						candidate := TransferredAsset{
							ID:           id,
							URL:          ref,
							Bucket:       assetBucket(ref),
							File:         filepath.Base(sourcePath),
							Role:         "route-transfer",
							SourcePath:   sizeEvidenceSourcePath(distDir, sourcePath, portableLabel),
							ManifestHash: hashed.Hash,
							SHA256:       metrics.SHA256,
							Bytes:        metrics.Bytes,
							GzipBytes:    metrics.GzipBytes,
							BrotliBytes:  metrics.BrotliBytes,
						}
						assetIndex = -1
						for i := range report.Assets {
							if report.Assets[i].ID == id {
								assetIndex = i
								break
							}
						}
						if assetIndex < 0 {
							report.Assets = append(report.Assets, candidate)
							assetIndex = len(report.Assets) - 1
						}
						assetFound = true
						byURL[ref] = assetIndex
					}
				}
			}
			if !assetFound {
				report.Unresolved = append(report.Unresolved, UnresolvedAssetRef{Ref: ref, Route: route.Path, Reason: "referenced by emitted HTML but not resolved"})
			}
			if !assetFound {
				continue
			}
			asset := &report.Assets[assetIndex]
			if seen[asset.ID] {
				continue
			}
			seen[asset.ID] = true
			entry.AssetIDs = append(entry.AssetIDs, asset.ID)
			asset.UsedByRoutes = append(asset.UsedByRoutes, route.Path)
			entry.RawBytes += asset.Bytes
			entry.GzipBytes += asset.GzipBytes
			entry.BrotliBytes += asset.BrotliBytes
		}
		sort.Strings(entry.AssetIDs)
		if canonical && len(entry.AssetIDs) == 0 && !routeAllowsNoRuntimeAttribution(route) {
			return fmt.Errorf("route %s has no resolved assets", route.Path)
		}
		if canonical {
			entry.ID = canonicalOuroborosRouteID(route.Path)
		}
		report.Routes = append(report.Routes, entry)
	}
	for i := range report.Routes {
		route := &report.Routes[i]
		route.SharedRawBytes = 0
		route.SharedGzipBytes = 0
		route.SharedBrotliBytes = 0
		route.UniqueRawBytes = 0
		route.UniqueGzipBytes = 0
		route.UniqueBrotliBytes = 0
		route.AttributionComment = ""
		for _, assetID := range route.AssetIDs {
			count := 0
			var asset *TransferredAsset
			for j := range report.Assets {
				if report.Assets[j].ID == assetID {
					asset = &report.Assets[j]
					count = len(report.Assets[j].UsedByRoutes)
					break
				}
			}
			if asset == nil {
				continue
			}
			if count > 1 {
				route.SharedRawBytes += asset.Bytes
				route.SharedGzipBytes += asset.GzipBytes
				route.SharedBrotliBytes += asset.BrotliBytes
			} else {
				route.UniqueRawBytes += asset.Bytes
				route.UniqueGzipBytes += asset.GzipBytes
				route.UniqueBrotliBytes += asset.BrotliBytes
			}
		}
		if route.SharedRawBytes > 0 {
			route.AttributionComment = "Shared route assets are counted in route totals and de-duplicated in report totals."
		}
	}
	return nil
}

func validateExportHTMLAttribution(distDir string, exportManifest exportEvidence) error {
	for _, route := range exportManifest.routes {
		htmlPath, err := routeHTMLPath(distDir, route.File)
		if err != nil {
			return fmt.Errorf("route %s HTML: %w", route.Path, err)
		}
		if refs := refsFromHTMLFile(htmlPath); len(refs) == 0 && !routeAllowsNoRuntimeAttribution(route) {
			return fmt.Errorf("route %s has incomplete asset attribution", route.Path)
		}
	}
	return nil
}

func routeHTMLPath(distDir, routeFile string) (string, error) {
	if strings.TrimSpace(routeFile) == "" {
		return "", fmt.Errorf("missing route file")
	}
	candidates := []struct {
		root string
		rel  string
	}{
		{root: distDir, rel: filepath.FromSlash(routeFile)},
		{root: filepath.Join(distDir, "static"), rel: filepath.FromSlash(routeFile)},
	}
	for _, candidate := range candidates {
		htmlPath, err := containedPath(candidate.root, candidate.rel)
		if err == nil {
			return htmlPath, nil
		}
	}
	return "", fmt.Errorf("missing or unsafe route file %s", routeFile)
}

func refsFromHTMLFile(file string) []string {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	input := string(data)
	refs := map[string]struct{}{}
	for _, token := range []string{"/gosx/", "gosx/"} {
		rest := input
		for {
			idx := strings.Index(rest, token)
			if idx < 0 {
				break
			}
			rest = rest[idx:]
			end := 0
			for end < len(rest) && isAssetURLByte(rest[end]) {
				end++
			}
			if ref := normalizeGosxRef(rest[:end]); ref != "" {
				refs[ref] = struct{}{}
			}
			rest = rest[end:]
		}
	}
	out := make([]string, 0, len(refs))
	for ref := range refs {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func directAssetSource(distDir string, manifest *buildmanifest.Manifest, rel string) (string, buildmanifest.HashedAsset, bool) {
	cleanRel := path.Clean("/" + strings.TrimLeft(rel, "/"))
	cleanRel = strings.TrimLeft(cleanRel, "/")
	if cleanRel == "" {
		return "", buildmanifest.HashedAsset{}, false
	}
	full, err := containedPath(filepath.Join(distDir, "assets"), cleanRel)
	if err != nil {
		return "", buildmanifest.HashedAsset{}, false
	}
	file := filepath.Base(full)
	for _, asset := range allManifestAssets(manifest) {
		if asset.File == file {
			return full, asset, true
		}
	}
	return full, buildmanifest.HashedAsset{File: file}, true
}

func allManifestAssets(manifest *buildmanifest.Manifest) []buildmanifest.HashedAsset {
	if manifest == nil {
		return nil
	}
	rt := manifest.Runtime
	out := []buildmanifest.HashedAsset{
		rt.WASM, rt.WASMIslands, rt.WASMExec, rt.StandardGoWASMExec,
		rt.Bootstrap, rt.BootstrapLite, rt.BootstrapRuntime,
		rt.BootstrapFeatureIslands, rt.BootstrapFeatureEngines, rt.BootstrapFeatureHubs,
		rt.BootstrapFeatureControllers, rt.BootstrapFeatureTextlayout,
		rt.BootstrapFeatureScene3D, rt.BootstrapFeatureScene3DCommand,
		rt.BootstrapFeatureScene3DWebGPU, rt.BootstrapFeatureScene3DWebGL,
		rt.BootstrapFeatureScene3DGLTF, rt.BootstrapFeatureScene3DAnimation,
		rt.BootstrapFeatureScene3DCompute, rt.BootstrapFeatureScene3DDecompress,
		rt.Patch, rt.VideoHLS, rt.StripeBridge, rt.Relay,
	}
	for _, asset := range manifest.Islands {
		out = append(out, asset.HashedAsset)
	}
	for _, asset := range manifest.CSS {
		out = append(out, asset.HashedAsset)
	}
	return out
}

func containedPath(root, rel string) (string, error) {
	full, err := safeJoin(root, rel)
	if err != nil {
		return "", err
	}
	rootEval, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	fullEval, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	relBack, err := filepath.Rel(rootEval, fullEval)
	if err != nil || relBack == ".." || strings.HasPrefix(relBack, ".."+string(filepath.Separator)) || filepath.IsAbs(relBack) {
		return "", fmt.Errorf("asset path escapes root: %s", rel)
	}
	return fullEval, nil
}

func collectBuildInputEvidence(repoRoot, manifestPath, exportPath string) (BuildInputEvidence, error) {
	root := repoRoot
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return BuildInputEvidence{}, err
	}
	moduleDir := commandOutput(rootAbs, "go", "list", "-f", "{{.Dir}}", "m31labs.dev/gosx")
	if moduleDir == "" {
		moduleDir = rootAbs
	}
	moduleDirAbs, err := filepath.Abs(moduleDir)
	if err != nil {
		return BuildInputEvidence{}, err
	}
	if moduleDirAbs != rootAbs {
		return BuildInputEvidence{}, fmt.Errorf("gosx module dir mismatch: current repo %s go list %s", rootAbs, moduleDirAbs)
	}
	out := BuildInputEvidence{
		GoSXModuleDir:              moduleDirAbs,
		GoSXModuleVersion:          commandOutput(rootAbs, "go", "list", "-m", "-f", "{{.Version}}", "m31labs.dev/gosx"),
		GoWork:                     commandOutput(rootAbs, "go", "env", "GOWORK"),
		RejectsModuleCacheMismatch: true,
	}
	out.GoModSHA256, _ = fileSHA256(filepath.Join(moduleDirAbs, "go.mod"))
	out.GoSumSHA256, _ = fileSHA256(filepath.Join(moduleDirAbs, "go.sum"))
	if out.GoWork != "" && out.GoWork != "off" {
		out.GoWorkSHA256, _ = fileSHA256(out.GoWork)
	}
	out.ManifestSHA256, _ = fileSHA256(manifestPath)
	if _, err := os.Stat(exportPath); err == nil {
		out.ExportSHA256, _ = fileSHA256(exportPath)
	}
	return out, nil
}

func BuildInputEvidenceForRepo(repoRoot, manifestPath, exportPath string) (BuildInputEvidence, error) {
	return collectBuildInputEvidence(repoRoot, manifestPath, exportPath)
}

func BuildSourceIdentity(ctx context.Context, repoRoot, inventoryPath, artifactRoot string) (SourceIdentity, error) {
	if strings.TrimSpace(artifactRoot) == "" {
		var err error
		artifactRoot, err = os.MkdirTemp("", "gosx-ouroboros-source-*")
		if err != nil {
			return SourceIdentity{}, err
		}
	}
	root, err := resolveRepoRootForEvidence(repoRoot)
	if err != nil {
		return SourceIdentity{}, err
	}
	return browserSourceIdentity(ctx, BrowserBaselineOptions{
		RepoRoot:      root,
		InventoryPath: inventoryPath,
		ArtifactRoot:  artifactRoot,
	})
}

func commandOutput(dir string, name string, args ...string) string {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func contextBackground() context.Context {
	return context.Background()
}

func isAssetURLByte(ch byte) bool {
	if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' {
		return true
	}
	switch ch {
	case '/', '.', '_', '-', '~', '%', '?', '&', '=', ':', '+', '#', ';', ',', '@', '!', '$', '\'', '(', ')', '*':
		return true
	default:
		return false
	}
}

func fillSizeEvidenceTotals(report *SizeEvidence) {
	seen := map[string]bool{}
	for _, asset := range report.Assets {
		report.Totals.AssetCount++
		report.Totals.RawBytes += asset.Bytes
		report.Totals.GzipBytes += asset.GzipBytes
		report.Totals.BrotliBytes += asset.BrotliBytes
		if !seen[asset.SHA256] {
			seen[asset.SHA256] = true
			report.Totals.DistinctContentCount++
			report.Totals.DistinctRawBytes += asset.Bytes
			report.Totals.DistinctGzipBytes += asset.GzipBytes
			report.Totals.DistinctBrotliBytes += asset.BrotliBytes
		}
	}
	report.Totals.RouteCount = len(report.Routes)
	for _, route := range report.Routes {
		if len(route.AssetIDs) > 0 {
			report.Totals.RoutesWithExplicitRefs++
		}
	}
	for i := range report.Assets {
		sort.Strings(report.Assets[i].UsedByRoutes)
	}
}

func CopyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
