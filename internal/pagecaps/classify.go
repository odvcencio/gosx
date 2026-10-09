package pagecaps

import (
	"errors"
	"sort"
)

const (
	engineJS uint16 = 1 << iota
	engineShared
	sceneJS
	sceneShared
	standardGo
	videoEngine
)

const (
	runtimeJS uint8 = 1 << iota
	runtimeShared
	runtimeGoWASM
)

// Classify returns every applicable obligation in stable order. Declaring a
// game adds a requirement; it cannot suppress islands or other experiences.
func Classify(c Capabilities, declaredGame bool) ([]string, error) {
	for _, count := range []int{c.Islands, c.ComputeIslands, c.Engines, c.Hubs, c.Controllers} {
		if count < 0 || count > 1000000 {
			return nil, errors.New("invalid capability count")
		}
	}
	mode := c.BootstrapMode
	if mode == "" {
		mode = "none"
	}
	switch mode {
	case "none", "lite", "full", "preview":
	default:
		return nil, errors.New("invalid bootstrap mode")
	}
	runtime := c.Runtime
	if runtime == "" {
		runtime = "none"
	}
	switch runtime {
	case "none", "js", "shared", "go-wasm", "mixed":
	default:
		return nil, errors.New("invalid runtime class")
	}
	if (runtime == "shared" || runtime == "go-wasm" || runtime == "mixed") && !c.WASM {
		return nil, errors.New("runtime requires WASM capability")
	}
	if declaredGame && c.Engines == 0 {
		return nil, errors.New("game requires an engine")
	}
	classes := map[string]bool{}
	add := func(name string) { classes[name] = true }
	if c.Islands > 0 || c.ComputeIslands > 0 {
		add("island")
	}
	if mode == "preview" {
		add("preview")
	}
	if c.Video {
		add("video")
	}
	variants := []string{"js"}
	if runtime == "shared" {
		variants = []string{"shared"}
	} else if runtime == "mixed" {
		variants = []string{"js", "shared"}
	}
	bits := c.engineTypes
	if !c.decoded {
		// Aggregate callers cannot distinguish the engine kinds in a mixed page.
		// Retain every possible runtime obligation in that case.
		if runtime == "go-wasm" || runtime == "mixed" {
			bits |= standardGo
		}
		// Experience flags can come from independent markup, so they cannot
		// establish how many registered engines have a specialized kind.
		if c.Engines > 0 && runtime != "go-wasm" {
			for _, variant := range variants {
				if variant == "shared" {
					bits |= engineShared
				} else {
					bits |= engineJS
				}
			}
		}
	}
	for _, entry := range []struct {
		bit  uint16
		name string
	}{
		{engineJS, "engine/js"}, {engineShared, "engine/shared"},
		{sceneJS, "scene3d/js"}, {sceneShared, "scene3d/shared"},
		{standardGo, "go-wasm"}, {videoEngine, "video"},
	} {
		if bits&entry.bit != 0 {
			add(entry.name)
		}
	}
	if c.Scene3D && bits&(sceneJS|sceneShared) == 0 {
		for _, variant := range variants {
			add("scene3d/" + variant)
		}
	}
	if declaredGame {
		if c.decoded {
			variants = nil
			if c.engineRuntimes&(runtimeJS|runtimeGoWASM) != 0 {
				variants = append(variants, "js")
			}
			if c.engineRuntimes&runtimeShared != 0 {
				variants = append(variants, "shared")
			}
		}
		for _, variant := range variants {
			add("game/" + variant)
		}
	}
	if len(classes) == 0 {
		if c.WASM {
			return nil, errors.New("WASM requires a declared experience")
		}
		if c.Navigation || c.Bootstrap || c.Motion || c.Hubs > 0 || c.Controllers > 0 || runtime == "js" {
			add("enhanced")
		} else {
			add("static")
		}
	}
	result := make([]string, 0, len(classes))
	for name := range classes {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}
