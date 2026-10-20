package budget

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

func TestMeasureCSSParseFailureKeepsConservativeCosts(t *testing.T) {
	for name, css := range map[string]string{
		"nested-selector":     `:global(html:has(.card)){color:blue}`,
		"invalid-declaration": `.broken{color:}`,
	} {
		t.Run(name, func(t *testing.T) {
			opts, manifest, document, _ := testRouteMeasurement(t)
			style, potential := []byte(css), []byte("body{color:red}")
			manifest.Assets = []buildmanifest.PerfAssetUse{manifest.Assets[0],
				graphAsset("app/fixture/public/style.css", "/style.css", "css", "startup", "always", style),
				graphAsset("app/fixture/public/potential.css", "/potential.css", "css", "dormant", "always", potential)}
			for path, body := range map[string][]byte{"style.css": style, "potential.css": potential} {
				if err := os.WriteFile(filepath.Join(opts.DistDir, path), body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			writeTestFixtureManifest(t, opts.DistDir, manifest)
			opts.BaseURL, opts.Client = servedMeasurementBodies(t, map[string][]byte{"/counter/": document, "/style.css": style, "/potential.css": potential})
			report, err := measureApp(context.Background(), opts, testBodyNormalizer)
			if err != nil {
				t.Fatal("CSS uncertainty became a fixture rejection", err)
			}
			docSizes, _ := testBodyNormalizer(document)
			styleSizes, _ := testBodyNormalizer(style)
			potentialSizes, _ := testBodyNormalizer(potential)
			row := report.Rows[0]
			if report.Coverage.Reachability != "unknown" || row.Requests != 3 || row.NormalizedBytes != docSizes.Brotli+styleSizes.Brotli+potentialSizes.Brotli || row.PhaseBytes.Startup != styleSizes.Brotli+potentialSizes.Brotli || row.PhaseBytes.Dormant != 0 {
				t.Fatalf("CSS parse failure lost conservative accounting: %+v", report)
			}
			for _, policy := range row.Policies {
				if policy.Name == "declared-fetches" && policy.Passed {
					t.Fatal("CSS parse failure certified declared fetches")
				}
			}
		})
	}
}
