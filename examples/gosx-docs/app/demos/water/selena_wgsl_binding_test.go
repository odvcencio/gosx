package docs

// This file is an automated verification harness for the Selena-authored water
// shaders under shaders/jeantimex-water.selena/*.sel. It exists to guard an
// upcoming rework of the WebGPU renderer: today, nothing catches a mismatch
// between the WGSL Selena emits and the host binding descriptor
// (selena/bindings.Layout) that a WebGPU renderer would use to build bind
// groups and pack the uniform buffer. Two independent checks, both
// table-driven over every .sel file in the directory so new shaders are
// covered automatically:
//
//   - TestWaterSelenaWGSLValidatesWithNaga: every emitted WGSL artifact must be
//     syntactically/semantically valid per the reference `naga` validator
//     (skipped, not failed, when naga isn't installed).
//   - TestWaterSelenaWGSLDescriptorMatchesBindings: the compiled
//     bindings.Layout for each shader must agree with the WGSL Selena actually
//     emitted -- same @group/@binding for every texture/sampler pair, the same
//     read vs read_write access for feedback ping-pong storage buffers, the
//     grid uniform at its descriptor slot, and (critically) the uniform
//     block's Fields in the same order as the emitted struct's members, with
//     every declared `context` name present and tagged Class=="context".

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/selena"
)

// waterSelenaShaderFileNames lists every .sel source under
// shaders/jeantimex-water.selena/, sorted for deterministic subtest order. It
// reads the directory (not the hand-maintained waterSelenaShaders table in
// selena_glsl.go) so a new shader file is picked up without touching this
// test.
func waterSelenaShaderFileNames(t testing.TB) []string {
	t.Helper()
	entries, err := waterSelenaFS.ReadDir("shaders/jeantimex-water.selena")
	if err != nil {
		t.Fatalf("read shaders/jeantimex-water.selena: %v", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sel") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	return files
}

// --- Test A: naga validation --------------------------------------------

// resolveNagaBinary returns the path to the naga WGSL validator, checking
// $PATH first and then the known /home/draco/.cargo/bin/naga fallback. It
// returns "" if naga can't be found anywhere, so the caller can skip rather
// than fail when the validator isn't installed.
func resolveNagaBinary() string {
	if path, err := exec.LookPath("naga"); err == nil {
		return path
	}
	const fallback = "/home/draco/.cargo/bin/naga"
	if info, err := os.Stat(fallback); err == nil && !info.IsDir() {
		return fallback
	}
	return ""
}

// TestWaterSelenaWGSLValidatesWithNaga compiles every water .sel shader to
// WGSL and validates the emitted source with the reference `naga` CLI. This
// catches anything naga considers invalid WGSL (bad syntax, type errors,
// binding conflicts within one shader) that Selena's own compiler didn't
// reject. Skips (does not fail) when naga isn't on $PATH or at the known
// cargo install location, so environments without the Rust toolchain still
// get a green package test.
func TestWaterSelenaWGSLValidatesWithNaga(t *testing.T) {
	naga := resolveNagaBinary()
	if naga == "" {
		t.Skip("naga not installed")
	}
	files := waterSelenaShaderFileNames(t)
	if len(files) == 0 {
		t.Fatal("no .sel shaders found under shaders/jeantimex-water.selena/")
	}
	for _, file := range files {
		file := file
		t.Run(file, func(t *testing.T) {
			src, err := waterSelenaFS.ReadFile("shaders/jeantimex-water.selena/" + file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			result, err := selena.Compile(src, selena.CompileOptions{Targets: []selena.Target{selena.TargetWGSL}})
			if err != nil {
				t.Fatalf("compile %s to WGSL: %v", file, err)
			}
			artifact, ok := result.Artifact(selena.TargetWGSL)
			if !ok || strings.TrimSpace(artifact.Source) == "" {
				t.Fatalf("%s: selena did not emit a WGSL artifact", file)
			}

			dir := t.TempDir()
			wgslPath := filepath.Join(dir, strings.TrimSuffix(file, ".sel")+".wgsl")
			if err := os.WriteFile(wgslPath, []byte(artifact.Source), 0o644); err != nil {
				t.Fatalf("write %s: %v", wgslPath, err)
			}

			var stderr bytes.Buffer
			cmd := exec.Command(naga, wgslPath)
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("naga rejected emitted WGSL for %s (%v):\n%s", file, err, stderr.String())
			}
		})
	}
}

// --- Test B: descriptor <-> WGSL binding consistency ---------------------
//
// The regexes below parse the small, mechanically-emitted subset of WGSL that
// carries binding information: @group/@binding-annotated `var` declarations
// (textures, samplers, storage buffers, uniform blocks) and struct bodies. A
// full WGSL parser isn't needed -- Selena's WGSL emitter always renders these
// declarations as single, complete statements.

// wgslVarDecl is one `@group(G) @binding(B) var<...> name : type;` (or the
// address-space-free texture/sampler form `@group(G) @binding(B) var name :
// type;`) declaration found in emitted WGSL.
type wgslVarDecl struct {
	Group   int
	Binding int
	Access  string // "" for texture/sampler/uniform, "read"/"read_write" for storage
	Name    string
	Type    string // texture_2d<f32>, sampler, UserUniforms, ... (struct type name for uniforms)
}

var (
	// Texture and sampler declarations: no address space, e.g.
	//   @group(0) @binding(1) var tileTexture : texture_2d<f32>;
	//   @group(0) @binding(2) var tileTextureSampler : sampler;
	wgslTextureDeclRE = regexp.MustCompile(`@group\(\s*(\d+)\s*\)\s*@binding\(\s*(\d+)\s*\)\s*var\s+(\w+)\s*:\s*(texture_\w+(?:<[^>]*>)?|sampler)\s*;`)
	// Storage buffer declarations (feedback ping-pong state), e.g.
	//   @group(0) @binding(1) var<storage, read> inState : array<vec4<f32>>;
	//   @group(0) @binding(2) var<storage, read_write> outState : array<vec4<f32>>;
	wgslStorageDeclRE = regexp.MustCompile(`@group\(\s*(\d+)\s*\)\s*@binding\(\s*(\d+)\s*\)\s*var<storage,\s*(read_write|read)>\s*(\w+)\s*:\s*array<[^;]+>\s*;`)
	// Uniform declarations, e.g.
	//   @group(0) @binding(0) var<uniform> u : Uniforms;
	//   @group(0) @binding(13) var<uniform> _stateGrid : StateGrid;
	wgslUniformDeclRE = regexp.MustCompile(`@group\(\s*(\d+)\s*\)\s*@binding\(\s*(\d+)\s*\)\s*var<uniform>\s*(\w+)\s*:\s*(\w+)\s*;`)
	// Struct bodies, e.g. `struct Uniforms {\n  mvp : mat4x4<f32>,\n  ...\n};`.
	// Selena never nests struct/brace bodies inside a member declaration
	// (arrays use angle brackets), so a non-greedy match to the first `}` is
	// exact.
	wgslStructRE = regexp.MustCompile(`(?s)struct\s+(\w+)\s*\{(.*?)\}`)
)

func parseWGSLDecls(re *regexp.Regexp, src string, hasAccess bool) []wgslVarDecl {
	matches := re.FindAllStringSubmatch(src, -1)
	out := make([]wgslVarDecl, 0, len(matches))
	for _, m := range matches {
		group, _ := strconv.Atoi(m[1])
		binding, _ := strconv.Atoi(m[2])
		if hasAccess {
			out = append(out, wgslVarDecl{Group: group, Binding: binding, Access: m[3], Name: m[4]})
		} else {
			out = append(out, wgslVarDecl{Group: group, Binding: binding, Name: m[3], Type: m[4]})
		}
	}
	return out
}

func findDeclAt(decls []wgslVarDecl, group, binding int) (wgslVarDecl, bool) {
	for _, d := range decls {
		if d.Group == group && d.Binding == binding {
			return d, true
		}
	}
	return wgslVarDecl{}, false
}

// parseWGSLStructFields returns the ordered member names of `struct
// structName { ... };` in src, or ok==false if no such struct is declared.
func parseWGSLStructFields(src, structName string) (fields []string, ok bool) {
	for _, m := range wgslStructRE.FindAllStringSubmatch(src, -1) {
		if m[1] != structName {
			continue
		}
		for _, line := range strings.Split(m[2], "\n") {
			line = strings.TrimSpace(line)
			line = strings.TrimSuffix(line, ",")
			if line == "" {
				continue
			}
			idx := strings.Index(line, ":")
			if idx < 0 {
				continue
			}
			name := strings.TrimSpace(line[:idx])
			if name == "" {
				continue
			}
			fields = append(fields, name)
		}
		return fields, true
	}
	return nil, false
}

func expectedTextureWGSLPrefix(dimension string) string {
	if dimension == "cube" {
		return "texture_cube"
	}
	return "texture_2d"
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// TestWaterSelenaWGSLDescriptorMatchesBindings proves that the host binding
// descriptor (bindings.Layout) Selena hands the renderer is faithful to the
// WGSL it actually emitted for the same compile: every texture/sampler,
// feedback storage buffer, grid uniform, and the main uniform block (with its
// declared `context` fields) must appear in the WGSL at exactly the
// group/binding the descriptor says, with matching read/read_write access
// and, for the uniform block, the same field order. This is the exact class
// of bug (a bind-group/WGSL mismatch) that has zero coverage today.
func TestWaterSelenaWGSLDescriptorMatchesBindings(t *testing.T) {
	files := waterSelenaShaderFileNames(t)
	if len(files) == 0 {
		t.Fatal("no .sel shaders found under shaders/jeantimex-water.selena/")
	}
	for _, file := range files {
		file := file
		t.Run(file, func(t *testing.T) {
			src, err := waterSelenaFS.ReadFile("shaders/jeantimex-water.selena/" + file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			result, err := selena.Compile(src, selena.CompileOptions{Targets: []selena.Target{selena.TargetWGSL}})
			if err != nil {
				t.Fatalf("compile %s to WGSL: %v", file, err)
			}
			artifact, ok := result.Artifact(selena.TargetWGSL)
			if !ok || strings.TrimSpace(artifact.Source) == "" {
				t.Fatalf("%s: selena did not emit a WGSL artifact", file)
			}
			wgsl := artifact.Source
			layout := result.Layout

			textureDecls := parseWGSLDecls(wgslTextureDeclRE, wgsl, false)
			storageDecls := parseWGSLDecls(wgslStorageDeclRE, wgsl, true)
			uniformDecls := parseWGSLDecls(wgslUniformDeclRE, wgsl, false)

			// Every descriptor texture must be declared at its WGSL
			// group/textureBinding with the right dimension, and its sampler
			// at group/samplerBinding.
			for _, tex := range layout.Textures {
				texDecl, ok := findDeclAt(textureDecls, tex.WGSL.Group, tex.WGSL.TextureBinding)
				if !ok {
					t.Fatalf("%s: descriptor texture %q expects @group(%d) @binding(%d) but WGSL has no texture declaration there", file, tex.Name, tex.WGSL.Group, tex.WGSL.TextureBinding)
				}
				if texDecl.Name != tex.Name {
					t.Fatalf("%s: WGSL @group(%d) @binding(%d) declares texture %q, descriptor says %q", file, tex.WGSL.Group, tex.WGSL.TextureBinding, texDecl.Name, tex.Name)
				}
				wantPrefix := expectedTextureWGSLPrefix(tex.Dimension)
				if !strings.HasPrefix(texDecl.Type, wantPrefix) {
					t.Fatalf("%s: texture %q declared as %q in WGSL, want dimension %q (prefix %q)", file, tex.Name, texDecl.Type, tex.Dimension, wantPrefix)
				}

				samplerName := tex.Name + "Sampler"
				samplerDecl, ok := findDeclAt(textureDecls, tex.WGSL.Group, tex.WGSL.SamplerBinding)
				if !ok {
					t.Fatalf("%s: descriptor sampler for texture %q expects @group(%d) @binding(%d) but WGSL has no sampler declaration there", file, tex.Name, tex.WGSL.Group, tex.WGSL.SamplerBinding)
				}
				if samplerDecl.Name != samplerName || samplerDecl.Type != "sampler" {
					t.Fatalf("%s: WGSL @group(%d) @binding(%d) declares %q : %q, want sampler %q : sampler", file, tex.WGSL.Group, tex.WGSL.SamplerBinding, samplerDecl.Name, samplerDecl.Type, samplerName)
				}
			}

			// A statefield's in-binding depends on WGSLStateBinding.InKind:
			//
			//   "texture" (render materials) — a texture_2d<f32> read with textureLoad.
			//     Their stateAt() taps are dependent chains, so the reads must go through
			//     the texture cache; a flat storage-buffer index bypasses it entirely.
			//     This is what the GL backend has always done via a sampler2D.
			//   "storage" (feedback materials) — the read buffer paired with the
			//     read_write out buffer the dispatch writes.
			for _, state := range layout.States {
				if state.WGSL.InKind == "texture" {
					inTex, ok := findDeclAt(textureDecls, state.WGSL.Group, state.WGSL.InBinding)
					if !ok {
						t.Fatalf("%s: state %q expects a texture at @group(%d) @binding(%d) but WGSL has none", file, state.Name, state.WGSL.Group, state.WGSL.InBinding)
					}
					if inTex.Type != "texture_2d<f32>" {
						t.Fatalf("%s: state %q in-texture at @group(%d) @binding(%d) is %q, want texture_2d<f32>", file, state.Name, state.WGSL.Group, state.WGSL.InBinding, inTex.Type)
					}
					if state.WGSL.OutBinding >= 0 {
						t.Fatalf("%s: state %q is texture-backed (read-only) but declares out-binding %d", file, state.Name, state.WGSL.OutBinding)
					}
					continue
				}
				inDecl, ok := findDeclAt(storageDecls, state.WGSL.Group, state.WGSL.InBinding)
				if !ok {
					t.Fatalf("%s: state %q expects a read storage buffer at @group(%d) @binding(%d) but WGSL has none", file, state.Name, state.WGSL.Group, state.WGSL.InBinding)
				}
				if inDecl.Access != "read" {
					t.Fatalf("%s: state %q in-buffer at @group(%d) @binding(%d) has access %q, want read", file, state.Name, state.WGSL.Group, state.WGSL.InBinding, inDecl.Access)
				}
				if state.WGSL.OutBinding >= 0 {
					outDecl, ok := findDeclAt(storageDecls, state.WGSL.Group, state.WGSL.OutBinding)
					if !ok {
						t.Fatalf("%s: state %q expects a read_write storage buffer at @group(%d) @binding(%d) but WGSL has none", file, state.Name, state.WGSL.Group, state.WGSL.OutBinding)
					}
					if outDecl.Access != "read_write" {
						t.Fatalf("%s: state %q out-buffer at @group(%d) @binding(%d) has access %q, want read_write", file, state.Name, state.WGSL.Group, state.WGSL.OutBinding, outDecl.Access)
					}
				}
			}

			// The grid uniform (feedback dispatch dimensions) must be
			// declared at its descriptor slot.
			if layout.Grid != nil {
				if _, ok := findDeclAt(uniformDecls, layout.Grid.WGSL.Group, layout.Grid.WGSL.Binding); !ok {
					t.Fatalf("%s: grid uniform expects @group(%d) @binding(%d) but WGSL has no uniform declaration there", file, layout.Grid.WGSL.Group, layout.Grid.WGSL.Binding)
				}
			}

			// The main uniform block: declared at its descriptor slot, and
			// its struct's ordered member names match UniformBlock.Fields
			// exactly. Selena omits the struct entirely when there are no
			// user uniforms (UniformBlock.Fields is empty) -- nothing to
			// check in that case.
			var structFields []string
			if len(layout.UniformBlock.Fields) > 0 {
				uDecl, ok := findDeclAt(uniformDecls, layout.WGSL.Group, layout.WGSL.Binding)
				if !ok {
					t.Fatalf("%s: uniform block expects @group(%d) @binding(%d) but WGSL has no uniform declaration there", file, layout.WGSL.Group, layout.WGSL.Binding)
				}
				fields, ok := parseWGSLStructFields(wgsl, uDecl.Type)
				if !ok {
					t.Fatalf("%s: WGSL declares uniform %q : %q but no `struct %s { ... }` body was found", file, uDecl.Name, uDecl.Type, uDecl.Type)
				}
				structFields = fields

				wantNames := make([]string, len(layout.UniformBlock.Fields))
				for i, f := range layout.UniformBlock.Fields {
					wantNames[i] = f.Name
				}
				if len(fields) != len(wantNames) {
					t.Fatalf("%s: WGSL struct %q has %d fields %v, descriptor UniformBlock has %d fields %v", file, uDecl.Type, len(fields), fields, len(wantNames), wantNames)
				}
				for i, want := range wantNames {
					if fields[i] != want {
						t.Fatalf("%s: WGSL struct %q field[%d] = %q, descriptor UniformBlock.Fields[%d] = %q (order mismatch)", file, uDecl.Type, i, fields[i], i, want)
					}
				}
			}

			// Every declared context field must be tagged Class=="context"
			// in the descriptor and present in the emitted uniform struct.
			for _, name := range layout.Context {
				var found bool
				var class string
				for _, f := range layout.UniformBlock.Fields {
					if f.Name == name {
						found = true
						class = f.Class
						break
					}
				}
				if !found {
					t.Fatalf("%s: context field %q is not present in descriptor UniformBlock.Fields", file, name)
				}
				if class != "context" {
					t.Fatalf("%s: context field %q has UniformBlock.Fields Class=%q, want \"context\"", file, name, class)
				}
				if !containsString(structFields, name) {
					t.Fatalf("%s: context field %q is not a member of the emitted WGSL uniform struct (fields: %v)", file, name, structFields)
				}
			}
		})
	}
}
