package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"m31labs.dev/gosx/dev"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/selenabuild"
)

// devSelenaBuild runs the Selena build step when gosx dev stages assets, so
// the app it starts embeds artifacts that match their .sel sources, as gosx
// build does. build.selena.disabled skips it here too.
func devSelenaBuild(dir string, out io.Writer) error {
	cfg, err := loadProjectConfig(dir)
	if err != nil {
		return err
	}
	return runSelenaBuildStep(dir, cfg.Build.Selena, out)
}

// devSelenaChange returns the dev server's SelenaChange hook. It rebuilds the
// project's artifacts with the gosx.config.json options and returns the new
// shaders of every changed source, for the page to swap in. It returns nil
// when build.selena.disabled is set, so .sel saves fall back to a reload.
func devSelenaChange(dir string) func([]string) ([]dev.SelenaUpdate, error) {
	cfg, err := loadProjectConfig(dir)
	if err != nil || cfg.Build.Selena.Disabled {
		return nil
	}
	return func(paths []string) ([]dev.SelenaUpdate, error) {
		cfg, err := loadProjectConfig(dir)
		if err != nil {
			return nil, err
		}
		if _, err := selenabuild.Build(dir, cfg.Build.Selena); err != nil {
			return nil, err
		}
		var updates []dev.SelenaUpdate
		for _, path := range paths {
			rel, err := filepath.Rel(dir, path)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			update, ok, err := selenaHotSwapUpdate(dir, filepath.ToSlash(rel))
			if err != nil {
				return nil, err
			}
			if ok {
				updates = append(updates, update)
			}
		}
		return updates, nil
	}
}

// selenaHotSwapUpdate reads the artifact Build wrote for one source and lowers
// each material to the Scene3D shader fields the dev script patches. A source
// with no artifact (a function library, or a deleted file) yields ok false.
func selenaHotSwapUpdate(dir, rel string) (dev.SelenaUpdate, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)+".json"))
	if os.IsNotExist(err) {
		return dev.SelenaUpdate{}, false, nil
	}
	if err != nil {
		return dev.SelenaUpdate{}, false, err
	}
	file, err := scene.DecodeSelenaArtifact(data)
	if err != nil {
		return dev.SelenaUpdate{}, false, fmt.Errorf("selena %s: %w", rel, err)
	}
	update := dev.SelenaUpdate{Source: rel}
	for _, name := range file.Names() {
		material, _, err := scene.MaterialFromSelenaArtifact(file, scene.SelenaMaterialOptions{
			Material:    name,
			SpecularMRT: file.Options.SpecularMRT && slices.Contains(selenaSpecularKinds, selenaKind(file, name)),
		})
		if err != nil {
			return dev.SelenaUpdate{}, false, fmt.Errorf("selena %s: %w", rel, err)
		}
		update.Materials = append(update.Materials, map[string]any{
			"material":             name,
			"customVertex":         material.VertexGLSL,
			"customFragment":       material.FragmentGLSL,
			"customVertexWGSL":     material.VertexWGSL,
			"customFragmentWGSL":   material.FragmentWGSL,
			"specularFragmentGLSL": material.SpecularFragmentGLSL,
			"specularFragmentWGSL": material.SpecularFragmentWGSL,
			"shaderLayout":         material.ShaderLayout,
		})
	}
	return update, len(update.Materials) > 0, nil
}

// selenaSpecularKinds are the material kinds a SpecularMRT build carries the
// specular variant for.
var selenaSpecularKinds = []string{"", "mesh"}

func selenaKind(file scene.SelenaArtifact, name string) string {
	res, err := file.Result(name)
	if err != nil {
		return ""
	}
	return string(res.Layout.Kind)
}
