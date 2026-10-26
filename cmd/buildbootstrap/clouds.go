package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/selena"
	"m31labs.dev/selena/parse"
)

// Bind helper locals through Selena's library lowering, so an inlined hash
// computes each intermediate once instead of expanding it at every use.
func compileCloudSurface() (selena.Result, error) {
	program, err := parse.Program(scene.SelenaCloudSource)
	if err != nil {
		return selena.Result{}, err
	}
	for i := range program.Funcs {
		program.Funcs[i].BindLocals = true
	}
	return selena.CompileProgram(program, selena.CompileOptions{Targets: []selena.Target{selena.TargetGLSL, selena.TargetWGSL}})
}

// cloudLibrary adapts Selena's emitted post surface to the renderer's shared
// cloud function. It removes only the entry-point ABI; scattering stays in .sel.
func cloudLibrary(source string, wgsl bool) (string, error) {
	marker := "void main() {"
	if wgsl {
		marker = "@fragment fn fragmentMain(in : PostInput) -> @location(0) vec4<f32> {"
	}
	i := strings.Index(source, marker)
	if i < 0 || strings.Count(source, marker) != 1 {
		return "", fmt.Errorf("Selena cloud surface entry point changed")
	}
	body := strings.TrimSpace(source[i+len(marker):])
	if !strings.HasSuffix(body, "}") {
		return "", fmt.Errorf("Selena cloud surface body incomplete")
	}
	body = strings.TrimSuffix(body, "}")
	if wgsl {
		for _, name := range []string{"ray", "eye", "settings", "drift", "ambient", "sunLight", "sun", "horizon"} {
			body = strings.ReplaceAll(body, "u."+name, name)
		}
		if strings.Contains(body, "u.") || strings.Contains(body, "in.") {
			return "", fmt.Errorf("unbound Selena cloud ABI")
		}
		return "fn gosxCloudSurface(ray:vec3f,eye:vec3f,settings:vec4f,drift:vec4f,ambient:vec3f,sunLight:vec3f,sun:vec3f,horizon:vec3f)->vec4f {\n" + body + "}\n", nil
	}
	if strings.Count(body, "gl_FragColor = ") != 1 {
		return "", fmt.Errorf("Selena cloud output ABI changed")
	}
	body = strings.Replace(body, "gl_FragColor = ", "return ", 1)
	return "vec4 gosxCloudSurface(vec3 ray,vec3 eye,vec4 settings,vec4 drift,vec3 ambient,vec3 sunLight,vec3 sun,vec3 horizon) {\n" + body + "}\n", nil
}

func generateCloudLibrary(dir string, check bool) error {
	requested := map[string]bool{}
	for _, entry := range outputs {
		for _, source := range entry.sources {
			for _, backend := range []string{"webgl", "webgpu"} {
				if source.rel == "../runtime/scene3d/"+backend+"-atmosphere.ts" {
					requested[backend] = true
				}
			}
		}
	}
	if len(requested) == 0 {
		return nil
	}
	result, err := compileCloudSurface()
	if err != nil {
		return fmt.Errorf("Selena clouds: %w", err)
	}

	for _, a := range result.Artifacts {
		wgsl := a.Target == selena.TargetWGSL
		src := a.Fragment
		backend := "webgl"
		if wgsl {
			src = a.Source
			backend = "webgpu"
		}
		if !requested[backend] {
			continue
		}
		library, err := cloudLibrary(src, wgsl)
		if err != nil {
			return err
		}
		block := "// BEGIN SELENA CLOUDS: generated from scene/clouds.sel; do not edit.\n" + fmt.Sprintf("const SCENE_CLOUD_LIBRARY_%s = %s;\n", strings.ToUpper(string(a.Target)), strconv.Quote(library)) + "// END SELENA CLOUDS\n"
		path := filepath.Join(dir, "../runtime/scene3d/"+backend+"-atmosphere.ts")
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(current)
		start := strings.Index(text, "// BEGIN SELENA CLOUDS:")
		next := block + text
		if start >= 0 {
			end := strings.Index(text[start:], "// END SELENA CLOUDS\n")
			if end < 0 {
				return fmt.Errorf("incomplete Selena cloud block")
			}
			end += start + len("// END SELENA CLOUDS\n")
			next = text[:start] + block + text[end:]
		}
		if next == text {
			continue
		}
		if check {
			return fmt.Errorf("stale Selena cloud library; run make build-bootstrap")
		}
		if err := os.WriteFile(path, []byte(next), 0644); err != nil {
			return err
		}
	}
	return nil
}
