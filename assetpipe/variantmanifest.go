package assetpipe

import (
	"encoding/json"
	"sort"
	"strings"

	"m31labs.dev/gosx/assetpipe/variantsel"
)

// VariantManifestSchemaVersion versions the variant manifest contract.
const VariantManifestSchemaVersion = 2

// VariantManifest is the file a server reads to choose an asset variant without
// opening the asset.
//
// The manifest exists so selection costs one map lookup per asset per request.
// Every field a selector needs is present: the URI, the kind, the tier, and the
// capability list. Nothing in the manifest requires reading a KTX2 header, and
// nothing requires a round trip to the browser.
//
// Only built variants appear. A planned variant describes work the pipeline did
// not do, so publishing it would invite a 404.
type VariantManifest struct {
	SchemaVersion int `json:"schemaVersion"`
	// Generator names the producer, for a bug report.
	Generator string `json:"generator"`
	// Vocabulary lists every capability token the manifest uses. A consumer
	// can check its own vocabulary against this list at startup instead of
	// discovering a typo at request time.
	Vocabulary []string        `json:"vocabulary,omitempty"`
	Assets     []ManifestAsset `json:"assets,omitempty"`
}

// ManifestAsset is one source asset and its built variants.
type ManifestAsset struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Bytes int64  `json:"bytes,omitempty"`
	// MetadataURI names a sidecar with the full build record, including per
	// variant pixel dimensions and mip level counts. It is absent when the
	// asset has no sidecar.
	MetadataURI string            `json:"metadataUri,omitempty"`
	Variants    []ManifestVariant `json:"variants,omitempty"`
}

// ManifestVariant is one built file a selector may choose.
type ManifestVariant struct {
	URI                  string   `json:"uri"`
	Kind                 string   `json:"kind,omitempty"`
	Role                 string   `json:"role,omitempty"`
	ColorSpace           string   `json:"colorSpace,omitempty"`
	Channels             string   `json:"channels,omitempty"`
	View                 string   `json:"view,omitempty"`
	Format               string   `json:"format,omitempty"`
	MipLevels            int      `json:"mipLevels,omitempty"`
	Quality              string   `json:"quality,omitempty"`
	Compression          string   `json:"compression,omitempty"`
	Bytes                int64    `json:"bytes,omitempty"`
	SourceAction         string   `json:"sourceAction,omitempty"`
	RequiredCapabilities []string `json:"requiredCapabilities,omitempty"`
}

// BuildVariantManifest collects every built variant of a report.
//
// The function reads the report only. It writes nothing, and it invents nothing:
// a variant appears in the manifest exactly when Execute set its State to
// "built", which happens after the executor wrote the file.
func BuildVariantManifest(report Report) VariantManifest {
	manifest := VariantManifest{
		SchemaVersion: VariantManifestSchemaVersion,
		Generator:     "gosx assetpipe",
	}
	vocabulary := map[string]struct{}{}
	for _, asset := range report.Assets {
		entry := ManifestAsset{Path: asset.Path, Kind: asset.Kind, Bytes: asset.Bytes}
		for _, variant := range asset.Variants {
			if !variant.Exists() {
				continue
			}
			if isMetadataKind(variant.Kind) {
				entry.MetadataURI = variant.URI
				continue
			}
			for _, token := range variant.RequiredCapabilities {
				vocabulary[token] = struct{}{}
			}
			entry.Variants = append(entry.Variants, ManifestVariant{
				URI:                  variant.URI,
				Kind:                 variant.Kind,
				Role:                 variant.Role,
				ColorSpace:           variant.ColorSpace,
				Channels:             variant.Channels,
				View:                 variant.View,
				Format:               variant.Format,
				MipLevels:            variant.MipLevels,
				Quality:              variant.Quality,
				Compression:          variant.Compression,
				Bytes:                variant.Bytes,
				SourceAction:         variant.SourceAction,
				RequiredCapabilities: append([]string(nil), variant.RequiredCapabilities...),
			})
		}
		if len(entry.Variants) == 0 && entry.MetadataURI == "" {
			continue
		}
		sort.SliceStable(entry.Variants, func(i, j int) bool {
			left, right := qualityRank(entry.Variants[i].Quality), qualityRank(entry.Variants[j].Quality)
			if left != right {
				return left > right
			}
			return entry.Variants[i].URI < entry.Variants[j].URI
		})
		manifest.Assets = append(manifest.Assets, entry)
	}
	sort.Slice(manifest.Assets, func(i, j int) bool { return manifest.Assets[i].Path < manifest.Assets[j].Path })
	for token := range vocabulary {
		manifest.Vocabulary = append(manifest.Vocabulary, token)
	}
	sort.Strings(manifest.Vocabulary)
	return manifest
}

// MarshalVariantManifest renders a manifest as indented JSON with a trailing
// newline, which matches the other sidecars the pipeline writes.
func MarshalVariantManifest(manifest VariantManifest) ([]byte, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// SelectFromManifest resolves one asset path against a capability set.
//
// The ranking matches SelectVariant: a higher tier first, then the smaller file.
// The second return value is false when no variant fits, which means the caller
// keeps the source URI.
func SelectFromManifest(manifest VariantManifest, path, kind string, caps variantsel.Set) (ManifestVariant, bool) {
	path = strings.TrimSpace(path)
	kind = strings.TrimSpace(strings.ToLower(kind))
	for _, asset := range manifest.Assets {
		if asset.Path != path {
			continue
		}
		var eligible []ManifestVariant
		for _, variant := range asset.Variants {
			if kind != "" && strings.ToLower(variant.Kind) != kind {
				continue
			}
			if !caps.HasAll(variant.RequiredCapabilities) {
				continue
			}
			eligible = append(eligible, variant)
		}
		if len(eligible) == 0 {
			return ManifestVariant{}, false
		}
		sort.SliceStable(eligible, func(i, j int) bool {
			left, right := qualityRank(eligible[i].Quality), qualityRank(eligible[j].Quality)
			if left != right {
				return left > right
			}
			if eligible[i].Bytes != eligible[j].Bytes {
				return eligible[i].Bytes < eligible[j].Bytes
			}
			return eligible[i].URI < eligible[j].URI
		})
		return eligible[0], true
	}
	return ManifestVariant{}, false
}

// isMetadataKind reports whether a variant kind names a sidecar instead of an
// uploadable payload. A selector must never hand one of these to a texture
// binding.
func isMetadataKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "environment-metadata", "texture-metadata", "lod-metadata":
		return true
	default:
		return false
	}
}
