package budget

import (
	"io"
	"os"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/pagecaps"
)

// FixtureManifest is a private production-body contract. It is not a public
// measurement root; publication retains logical IDs and omits URLs.
type FixtureManifest struct {
	Schema         string                       `json:"schema"`
	Version        int64                        `json:"version"`
	Routes         []FixtureRoute               `json:"routes"`
	Assets         []buildmanifest.PerfAssetUse `json:"assets"`
	SourceSHA      string                       `json:"sourceSHA"`
	FixturesSHA256 string                       `json:"fixturesSHA256"`
	CatalogSHA256  string                       `json:"catalogSHA256"`
}
type FixtureRoute struct {
	App              string                `json:"app"`
	RouteTemplate    string                `json:"routeTemplate"`
	SourcePath       string                `json:"sourcePath"`
	PageTypes        []string              `json:"pageTypes"`
	Capabilities     pagecaps.Capabilities `json:"capabilities"`
	StartupCounts    Workload              `json:"startupCounts"`
	CriticalAssetIDs []string              `json:"criticalAssetIDs"`
	InputSequenceID  string                `json:"inputSequenceID"`
	GPUEstimate      *FixtureGPU           `json:"gpuEstimate"`
}
type FixtureGPU struct {
	InitialBytes     int64 `json:"initialBytes"`
	UnknownResources int64 `json:"unknownResources"`
	Complete         bool  `json:"complete"`
}

// DecodeFixtureManifest validates a bounded closed bundle contract, including
// global asset identities, ownership, dependencies and route declarations.
func DecodeFixtureManifest(r io.Reader) (*FixtureManifest, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxInputBytes+1))
	if err != nil || len(data) > maxInputBytes {
		return nil, measureFailure("wrong-fixture", "/manifest")
	}
	var manifest FixtureManifest
	if err := decodeInput(data, "FixtureManifest", &manifest); err != nil {
		return nil, err
	}
	if err := (&buildmanifest.Manifest{PerfAssetUses: &buildmanifest.PerfAssetUses{Version: 1, Assets: manifest.Assets}}).ValidatePerfAssetUses(); err != nil {
		return nil, measureFailure("wrong-fixture", "/manifest/assets")
	}
	seen := map[string]bool{}
	assets := map[string]bool{}
	for _, asset := range manifest.Assets {
		assets[asset.ID] = true
	}
	for _, route := range manifest.Routes {
		key := route.App + "|" + route.RouteTemplate
		if seen[key] || !validRoute(route.RouteTemplate) {
			return nil, measureFailure("wrong-fixture", "/manifest/routes")
		}
		seen[key] = true
		types := map[string]bool{}
		for _, name := range route.PageTypes {
			if !knownPageType(name) || types[name] {
				return nil, measureFailure("wrong-fixture", "/manifest/routes/pageTypes")
			}
			types[name] = true
		}
		for _, id := range route.CriticalAssetIDs {
			if !assets[id] {
				return nil, measureFailure("wrong-fixture", "/manifest/routes/criticalAssetIDs")
			}
		}
		if _, err := pagecaps.Classify(route.Capabilities, false); err != nil {
			return nil, measureFailure("wrong-fixture", "/manifest/routes/capabilities")
		}
	}
	return &manifest, nil
}

func readMeasureFile(root *os.Root, name string, limit int64) ([]byte, error) {
	if !safePath(name) {
		return nil, measureFailure("wrong-fixture", "/file")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, measureFailure("wrong-fixture", "/file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, measureFailure("wrong-fixture", "/file")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, measureFailure("wrong-fixture", "/file")
	}
	return data, nil
}
