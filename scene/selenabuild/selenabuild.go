// Package selenabuild compiles an application's Selena (.sel) sources into
// artifact files at build time and checks shipped artifact files against their
// sources.
//
// Each source path.sel gets an artifact path.sel.json next to it, so the Go
// package beside the source can embed it:
//
//	//go:embed rock.sel.json
//	var rockArtifact []byte
//
//	file, err := scene.DecodeSelenaArtifact(rockArtifact)
//	material, _, err := scene.MaterialFromSelenaArtifact(file, scene.SelenaMaterialOptions{})
//
// `gosx build` runs Build before it compiles Go code, and `gosx selena check`
// runs Check. An application can also call Check from a Go test so `go test`
// fails when someone edits a .sel file without rebuilding its artifact.
//
// This package links the Selena compiler. Import it only from build tools and
// tests, never from code that ships to the browser.
package selenabuild

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"m31labs.dev/selena/artifact"
	"m31labs.dev/selena/artifact/builder"
	"m31labs.dev/selena/materiallib"
	"m31labs.dev/selena/result"
)

// Options is build.selena in gosx.config.json.
type Options struct {
	// Targets lists the shader targets each artifact carries. Empty means
	// ["wgsl", "gles"]: the WebGPU and WebGL2 programs the browser renderers
	// run. Add "glsl" and "metal" to ship the WebGL1 and native programs too.
	Targets []string `json:"targets,omitempty"`
	// Modules lists Selena material library modules linked into every source,
	// for example ["lighting", "brdf"]. Linking a module a source does not call
	// changes nothing in its artifact.
	Modules []string `json:"modules,omitempty"`
	// SpecularMRT adds the color/specular fragment variant to mesh materials,
	// for scenes that use selective (specular) bloom.
	SpecularMRT bool `json:"specularMRT,omitempty"`
	// Disabled skips the Selena step in `gosx build`, for a project that
	// commits its artifacts or keeps .sel sources it does not want built.
	// `gosx selena build` and `gosx selena check` still run.
	Disabled bool `json:"disabled,omitempty"`
}

// DefaultTargets are the targets an artifact carries when Options.Targets is
// empty.
var DefaultTargets = []result.Target{result.TargetWGSL, result.TargetGLES}

// targets resolves Options.Targets.
func (o Options) targets() ([]result.Target, error) {
	if len(o.Targets) == 0 {
		return slices.Clone(DefaultTargets), nil
	}
	out := make([]result.Target, 0, len(o.Targets))
	for _, name := range o.Targets {
		t := result.Target(strings.TrimSpace(name))
		if !slices.Contains(result.AllTargets(), t) {
			return nil, fmt.Errorf("selena: unknown target %q (want wgsl, glsl, metal or gles)", name)
		}
		if slices.Contains(out, t) {
			return nil, fmt.Errorf("selena: target %q listed twice", name)
		}
		out = append(out, t)
	}
	return out, nil
}

func (o Options) modules() []materiallib.Module {
	if len(o.Modules) == 0 {
		return nil
	}
	out := make([]materiallib.Module, len(o.Modules))
	for i, m := range o.Modules {
		out[i] = materiallib.Module(strings.TrimSpace(m))
	}
	return out
}

// Report lists what Build did. Paths are slash-separated and relative to the
// root.
type Report struct {
	// Sources lists every .sel source found, sorted.
	Sources []string
	// Written lists the artifacts Build created or replaced.
	Written []string
	// Libraries lists sources that declare functions but no material. They
	// have no artifact of their own.
	Libraries []string
}

// Sources returns the .sel files under root, sorted and slash-separated
// relative to root. It skips directories Go ignores (names starting with "." or
// "_", and testdata), vendor, node_modules, dist, and nested Go modules, which
// build their own artifacts.
func Sources(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "testdata" || name == "vendor" || name == "node_modules" || name == "dist" {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && strings.HasSuffix(d.Name(), ".sel") {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(out)
	return out, err
}

// Build writes an artifact next to every .sel source under root. It rewrites a
// file only when its bytes change, so an unchanged source keeps its artifact's
// modification time and Go's build cache.
func Build(root string, opts Options) (Report, error) {
	targets, err := opts.targets()
	if err != nil {
		return Report{}, err
	}
	sources, err := Sources(root)
	if err != nil {
		return Report{}, err
	}
	report := Report{Sources: sources}
	for _, rel := range sources {
		source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return report, err
		}
		file, err := builder.Build(source, builder.Options{
			Path:        rel,
			Targets:     targets,
			SpecularMRT: opts.SpecularMRT,
			Modules:     opts.modules(),
		})
		if err != nil {
			if isLibrary(err) {
				report.Libraries = append(report.Libraries, rel)
				continue
			}
			return report, fmt.Errorf("selena %s: %w", rel, err)
		}
		data, err := artifact.Encode(file)
		if err != nil {
			return report, err
		}
		dest := filepath.Join(root, filepath.FromSlash(artifact.PathFor(rel)))
		if existing, err := os.ReadFile(dest); err == nil && bytes.Equal(existing, data) {
			continue
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return report, err
		}
		report.Written = append(report.Written, artifact.PathFor(rel))
	}
	return report, nil
}

// Problem is one artifact that does not match its source.
type Problem struct {
	// Source is the .sel path, slash-separated and relative to the root.
	Source string
	// Reason says what is wrong.
	Reason string
}

// DriftError lists every artifact under a root that is missing or no longer
// matches its source, its build options, or the linked compiler.
type DriftError struct {
	Problems []Problem
}

func (e *DriftError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d Selena artifact(s) out of date; run `gosx selena build`:", len(e.Problems))
	for _, p := range e.Problems {
		fmt.Fprintf(&b, "\n  %s: %s", p.Source, p.Reason)
	}
	return b.String()
}

// Check reports a *DriftError when any .sel source under root has no artifact,
// or an artifact built from other bytes, with other options than opts, or with
// output the linked compiler no longer emits. Library sources (functions only)
// are skipped. A different recorded Selena version alone is not drift.
func Check(root string, opts Options) error {
	targets, err := opts.targets()
	if err != nil {
		return err
	}
	want := artifact.Options{Targets: targets, SpecularMRT: opts.SpecularMRT, Modules: opts.Modules}
	if len(want.Modules) == 0 {
		want.Modules = nil
	}
	sources, err := Sources(root)
	if err != nil {
		return err
	}
	var drift DriftError
	for _, rel := range sources {
		source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(artifact.PathFor(rel))))
		if errors.Is(err, fs.ErrNotExist) {
			if isLibrarySource(source, rel, opts, targets) {
				continue
			}
			drift.Problems = append(drift.Problems, Problem{rel, "no artifact " + artifact.PathFor(rel)})
			continue
		}
		if err != nil {
			return err
		}
		shipped, err := artifact.Decode(data)
		if err != nil {
			drift.Problems = append(drift.Problems, Problem{rel, err.Error()})
			continue
		}
		if !sameOptions(shipped.Options, want) {
			drift.Problems = append(drift.Problems, Problem{rel, fmt.Sprintf("built with %s, config wants %s", describe(shipped.Options), describe(want))})
			continue
		}
		if err := builder.Check(shipped, source); err != nil {
			drift.Problems = append(drift.Problems, Problem{rel, err.Error()})
		}
	}
	if len(drift.Problems) > 0 {
		return &drift
	}
	return nil
}

func sameOptions(a, b artifact.Options) bool {
	return slices.Equal(a.Targets, b.Targets) && a.SpecularMRT == b.SpecularMRT && slices.Equal(a.Modules, b.Modules)
}

func describe(o artifact.Options) string {
	parts := make([]string, len(o.Targets))
	for i, t := range o.Targets {
		parts[i] = string(t)
	}
	out := "targets " + strings.Join(parts, ",")
	if len(o.Modules) > 0 {
		out += " modules " + strings.Join(o.Modules, ",")
	}
	if o.SpecularMRT {
		out += " specularMRT"
	}
	return out
}

// isLibrary reports a build failure caused only by a source with no material.
func isLibrary(err error) bool { return errors.Is(err, builder.ErrNoMaterial) }

// isLibrarySource reports whether source declares no material.
func isLibrarySource(source []byte, rel string, opts Options, targets []result.Target) bool {
	_, err := builder.Build(source, builder.Options{Path: rel, Targets: targets, Modules: opts.modules()})
	return isLibrary(err)
}
