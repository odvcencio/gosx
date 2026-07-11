// Package materials owns the portable Selena material families used by the
// Chinese Checkers showcase. Geometry and game rules deliberately live
// elsewhere: switching a family only changes optical response.
package materials

import (
	_ "embed"
	"fmt"
	"strings"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/selena/bindings"
)

type Family string

const (
	ImperialJade Family = "imperial-jade"
	CarvedWood   Family = "carved-wood"
	BrushedSteel Family = "brushed-steel"
)

//go:embed sources/imperial-jade.sel
var imperialJadeSource []byte

//go:embed sources/carved-wood.sel
var carvedWoodSource []byte

//go:embed sources/brushed-steel.sel
var brushedSteelSource []byte

// Profile keeps authored source, compiled artifacts, and honest fallback together.
type Profile struct {
	Family      Family
	Name        string
	Source      []byte
	Selena      scene.CustomMaterial
	Layout      bindings.Layout
	Fallback    scene.StandardMaterial
	FallbackFor string
}

// RuntimeCapabilities are selected-renderer facts, not browser API guesses.
type RuntimeCapabilities struct {
	SelenaMaterials bool
	Transmission    bool
	Anisotropy      bool
}

type Active struct {
	Profile  Profile
	Material any
	Backend  string
	Label    string
	Fallback bool
	Reason   string
}

func Families() []Family {
	return []Family{ImperialJade, CarvedWood, BrushedSteel}
}

func Compile(family Family) (Profile, error) {
	definition, err := definitionFor(family)
	if err != nil {
		return Profile{}, err
	}
	compiled, layout, err := scene.CompileSelenaMaterial(definition.source, scene.SelenaMaterialOptions{
		Material: definition.material,
		Standard: definition.standard,
	})
	if err != nil {
		return Profile{}, fmt.Errorf("compile checkers material %s: %w", family, err)
	}
	return Profile{
		Family:      family,
		Name:        definition.name,
		Source:      append([]byte(nil), definition.source...),
		Selena:      compiled,
		Layout:      layout,
		Fallback:    definition.fallback,
		FallbackFor: definition.fallbackFor,
	}, nil
}

func Resolve(family Family, caps RuntimeCapabilities) (Active, error) {
	profile, err := Compile(family)
	if err != nil {
		return Active{}, err
	}
	reason := ""
	if !caps.SelenaMaterials {
		reason = "portable Selena materials unavailable"
	} else if family == ImperialJade && !caps.Transmission {
		reason = "transmission unavailable"
	} else if family == BrushedSteel && !caps.Anisotropy {
		reason = "anisotropy unavailable"
	}
	if reason != "" {
		return Active{Profile: profile, Material: profile.Fallback, Backend: "standard-pbr", Label: profile.Name + " · PBR fallback", Fallback: true, Reason: reason}, nil
	}
	return Active{Profile: profile, Material: profile.Selena, Backend: "selena", Label: profile.Name + " · Selena"}, nil
}

type definition struct {
	name        string
	material    string
	source      []byte
	standard    scene.StandardMaterial
	fallback    scene.StandardMaterial
	fallbackFor string
}

func definitionFor(family Family) (definition, error) {
	switch family {
	case ImperialJade:
		return definition{name: "Imperial Jade", material: "ImperialJade", source: imperialJadeSource,
			standard:    scene.StandardMaterial{Color: "#4fa979", Roughness: 0.18, Metalness: 0.02, Clearcoat: 0.82, Transmission: 0.38},
			fallback:    scene.StandardMaterial{Color: "#397f5d", Roughness: 0.24, Metalness: 0.02, Clearcoat: 0.7},
			fallbackFor: "opaque jade with authored Fresnel rim and baked thickness tint"}, nil
	case CarvedWood:
		return definition{name: "Carved Wood", material: "CarvedWood", source: carvedWoodSource,
			standard:    scene.StandardMaterial{Color: "#70462f", Roughness: 0.58, Metalness: 0, Clearcoat: 0.12},
			fallback:    scene.StandardMaterial{Color: "#70462f", Roughness: 0.62, Metalness: 0},
			fallbackFor: "satin walnut PBR without procedural directional grain"}, nil
	case BrushedSteel:
		return definition{name: "Brushed Steel", material: "BrushedSteel", source: brushedSteelSource,
			standard:    scene.StandardMaterial{Color: "#aeb8bd", Roughness: 0.3, Metalness: 0.92, Anisotropy: 0.72},
			fallback:    scene.StandardMaterial{Color: "#939da3", Roughness: 0.38, Metalness: 0.88},
			fallbackFor: "metallic PBR with tangent-aligned brush contrast baked by Selena"}, nil
	default:
		return definition{}, fmt.Errorf("unknown checkers material family %q", strings.TrimSpace(string(family)))
	}
}
