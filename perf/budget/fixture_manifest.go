package budget

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/pagecaps"
	"m31labs.dev/gosx/internal/regularfile"
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
	if err := validateFixtureManifest(manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

// Both decoding and production use the same complete manifest contract checks.
func validateFixtureManifest(manifest FixtureManifest) error {
	if err := (&buildmanifest.Manifest{PerfAssetUses: &buildmanifest.PerfAssetUses{Version: 1, Assets: manifest.Assets}}).ValidatePerfAssetUses(); err != nil {
		return measureFailure("wrong-fixture", "/manifest/assets")
	}
	seen := map[string]bool{}
	assets := map[string]bool{}
	for _, asset := range manifest.Assets {
		assets[asset.ID] = true
	}
	for _, route := range manifest.Routes {
		key := route.App + "|" + route.RouteTemplate
		if seen[key] || !validRoute(route.RouteTemplate) {
			return measureFailure("wrong-fixture", "/manifest/routes")
		}
		seen[key] = true
		types := map[string]bool{}
		for _, name := range route.PageTypes {
			if !knownPageType(name) || types[name] {
				return measureFailure("wrong-fixture", "/manifest/routes/pageTypes")
			}
			types[name] = true
		}
		for _, id := range route.CriticalAssetIDs {
			if !assets[id] {
				return measureFailure("wrong-fixture", "/manifest/routes/criticalAssetIDs")
			}
		}
		if _, err := pagecaps.Classify(route.Capabilities, false); err != nil {
			return measureFailure("wrong-fixture", "/manifest/routes/capabilities")
		}
	}
	for _, route := range manifest.Routes {
		physical, err := fixturePhysicalAssets(fixtureAppAssets(manifest.Assets, route.App))
		if err != nil {
			return err
		}
		found := false
		for _, use := range physical {
			if use.URL == route.RouteTemplate {
				found = use.Owner == "app" && use.Kind == "html"
			}
		}
		if !found {
			return measureFailure("wrong-fixture", "/routes/document")
		}
	}

	digest, err := FixtureManifestSHA256(manifest)
	if err != nil || digest != manifest.FixturesSHA256 {
		return measureFailure("wrong-fixture", "/manifest/fixturesSHA256")
	}
	return nil
}

func readMeasureFile(root *os.Root, name string, limit int64) ([]byte, error) {
	if !safePath(name) {
		return nil, measureFailure("wrong-fixture", "/file")
	}
	file, err := regularfile.Open(root, name)
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

// FixtureManifestSHA256 hashes the complete producer contract independently
// of the stable catalog reference. Its own digest and fixed schema label are
// omitted; nested keys are canonical and integer values retain their spelling.
func FixtureManifestSHA256(manifest FixtureManifest) (string, error) {
	manifest.FixturesSHA256 = strings.Repeat("0", 64)
	if err := validateTyped("FixtureManifest", manifest); err != nil {
		return "", err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return "", measureFailure("wrong-fixture", "/manifest")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var fields map[string]any
	if decoder.Decode(&fields) != nil {
		return "", measureFailure("wrong-fixture", "/manifest")
	}
	delete(fields, "fixturesSHA256")
	delete(fields, "schema")
	data, err = json.Marshal(fields)
	if err != nil {
		return "", measureFailure("wrong-fixture", "/manifest")
	}
	sum := sha256.Sum256(append(data, '\n'))
	return hex.EncodeToString(sum[:]), nil
}
