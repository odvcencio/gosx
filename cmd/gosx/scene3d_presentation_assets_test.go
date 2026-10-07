package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScene3DPlaybackAssetsExportAndManifestCompatibility(t *testing.T) {
	for _, chunk := range []string{"presentation", "timeline", "particle-burst"} {
		t.Run(chunk, func(t *testing.T) {
			name := "bootstrap-feature-scene3d-" + chunk + ".js"
			ref := "/gosx/" + name
			refs := map[string]struct{}{}
			addExportRuntimeAssetRefs(refs, `<script data-gosx-scene3d-`+chunk+`-url="`+ref+`?v=abc"></script>`)
			if _, ok := refs[ref]; !ok {
				t.Fatal("export omitted the advertised lazy chunk")
			}
			buildDir, distDir := t.TempDir(), t.TempDir()
			hashed := strings.TrimSuffix(name, ".js") + ".abc.js"
			manifest := &BuildManifest{}
			asset := HashedAsset{File: hashed, Hash: "abc"}
			switch chunk {
			case "presentation":
				manifest.Runtime.BootstrapFeatureScene3DPresentation = asset
			case "timeline":
				manifest.Runtime.BootstrapFeatureScene3DTimeline = asset
			case "particle-burst":
				manifest.Runtime.BootstrapFeatureScene3DParticleBurst = asset
			}
			for _, suffix := range []string{"", ".gz", ".br"} {
				mustWriteFile(t, filepath.Join(buildDir, name+suffix), chunk+suffix)
				mustWriteFile(t, filepath.Join(distDir, "assets", "runtime", hashed+suffix), chunk+suffix)
			}
			for _, mode := range []string{"export", "manifest compatibility"} {
				output := t.TempDir()
				var err error
				if mode == "export" {
					err = copyExportRuntime(buildDir, output, exportManifest{AssetRefs: []string{ref}})
				} else {
					err = stageManifestCompatibilityRuntime(distDir, manifest, output, []string{ref})
				}
				if err != nil {
					t.Fatal(err)
				}
				for _, suffix := range []string{"", ".gz", ".br"} {
					body, err := os.ReadFile(filepath.Join(output, "gosx", name+suffix))
					if err != nil || string(body) != chunk+suffix {
						t.Fatalf("%s dropped %s: %q, %v", mode, suffix, body, err)
					}
				}
			}
		})
	}
}

func TestScene3DPresentationSizeReportIsOptional(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "assets", "runtime", "presentation.abc.js"), "presentation")
	mustWriteFile(t, filepath.Join(dir, "build.json"), `{"runtime":{"bootstrapFeatureScene3dPresentation":{"file":"presentation.abc.js"}}}`)
	report, err := buildSizeReport(dir)
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalBytes != int64(len("presentation")) || report.ColdStartBytes != 0 || len(report.Assets) != 1 {
		t.Fatalf("presentation must count toward total, without being a cold-start prerequisite: %+v", report)
	}
	for _, name := range runtimeExcludableAssetRoles["scene3d"] {
		if name == "bootstrap-feature-scene3d-presentation.js" {
			return
		}
	}
	t.Fatal("scene3d exclusion omitted presentation")
}
