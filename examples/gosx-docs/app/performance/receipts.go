package performance

import (
	"bytes"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

//go:embed receipts.json
var receiptFiles embed.FS

type Receipts struct {
	SchemaVersion int               `json:"schemaVersion"`
	MeasuredAt    time.Time         `json:"measuredAt"`
	Commit        string            `json:"commit"`
	Tree          string            `json:"tree"`
	FeaturedPath  string            `json:"featuredPath"`
	Machine       MachineReceipt    `json:"machine"`
	Lighthouse    LighthouseReceipt `json:"lighthouse"`
	GPU           GPUReceipt        `json:"gpu"`
	Bundles       []BundleReceipt   `json:"bundles"`
	Quickstart    QuickstartReceipt `json:"quickstart"`
}

type MachineReceipt struct {
	Name        string  `json:"name"`
	OS          string  `json:"os"`
	CPU         string  `json:"cpu"`
	MemoryGiB   float64 `json:"memoryGiB"`
	LoadAverage string  `json:"loadAverage"`
}

type LighthouseReceipt struct {
	Browser          string           `json:"browser"`
	Method           string           `json:"method"`
	LoadAverageStart string           `json:"loadAverageStart"`
	LoadAverageEnd   string           `json:"loadAverageEnd"`
	RunCount         int              `json:"runCount"`
	Pages            []LighthousePage `json:"pages"`
}

type LighthousePage struct {
	Path   string            `json:"path"`
	Runs   []LighthouseScore `json:"runs"`
	Median LighthouseScore   `json:"median"`
}

type LighthouseScore struct {
	Performance   int     `json:"performance"`
	Accessibility int     `json:"accessibility"`
	BestPractices int     `json:"bestPractices"`
	SEO           int     `json:"seo"`
	CLS           float64 `json:"cls"`
}

type GPUReceipt struct {
	Label   string         `json:"label"`
	Browser string         `json:"browser"`
	Method  string         `json:"method"`
	Scenes  []SceneReceipt `json:"scenes"`
}

type SceneReceipt struct {
	Path     string                `json:"path"`
	Title    string                `json:"title"`
	Backends []BackendFrameReceipt `json:"backends"`
}

type BackendFrameReceipt struct {
	Backend      string  `json:"backend"`
	FirstDrawMs  float64 `json:"firstDrawMs"`
	CadenceP50Ms float64 `json:"cadenceP50Ms"`
	CadenceP95Ms float64 `json:"cadenceP95Ms"`
	RafCPUP95Ms  float64 `json:"rafCpuP95Ms"`
}

type BundleReceipt struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	RawBytes    int64  `json:"rawBytes"`
	BrotliBytes int64  `json:"brotliBytes"`
}

type QuickstartReceipt struct {
	Command string  `json:"command"`
	Method  string  `json:"method"`
	ColdSec float64 `json:"coldSeconds"`
	WarmSec float64 `json:"warmSeconds"`
}

func Read() (Receipts, error) {
	data, err := receiptFiles.ReadFile("receipts.json")
	if err != nil {
		return Receipts{}, err
	}
	var receipts Receipts
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipts); err != nil {
		return Receipts{}, fmt.Errorf("decode performance receipts: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Receipts{}, fmt.Errorf("decode performance receipts: expected one JSON document")
	}
	return receipts, nil
}

func (r Receipts) Validate() error {
	if r.SchemaVersion != 1 {
		return fmt.Errorf("schemaVersion must be 1")
	}
	if r.MeasuredAt.IsZero() || len(r.Commit) != 40 {
		return fmt.Errorf("measurement timestamp and commit are required")
	}
	if _, err := hex.DecodeString(r.Commit); err != nil {
		return fmt.Errorf("measurement commit must be a hexadecimal Git SHA")
	}
	if r.Tree != "" {
		if len(r.Tree) != 40 {
			return fmt.Errorf("measurement tree must be a full Git SHA")
		}
		if _, err := hex.DecodeString(r.Tree); err != nil {
			return fmt.Errorf("measurement tree must be a hexadecimal Git SHA")
		}
	}
	if r.Machine.Name == "" || r.Machine.OS == "" || r.Machine.CPU == "" || r.Machine.MemoryGiB <= 0 || r.Machine.LoadAverage == "" {
		return fmt.Errorf("machine name, OS, CPU, memory, and load average are required")
	}
	if r.Lighthouse.Browser == "" || r.Lighthouse.Method == "" || r.Lighthouse.RunCount != 3 || len(r.Lighthouse.Pages) == 0 {
		return fmt.Errorf("Lighthouse requires browser, method, and three cold runs per page")
	}
	if (r.Lighthouse.LoadAverageStart == "") != (r.Lighthouse.LoadAverageEnd == "") {
		return fmt.Errorf("Lighthouse load average start and end must be recorded together")
	}
	paths := make(map[string]bool, len(r.Lighthouse.Pages))
	for _, page := range r.Lighthouse.Pages {
		if page.Path == "" || paths[page.Path] || len(page.Runs) != 3 {
			return fmt.Errorf("Lighthouse page %q must be unique and have three runs", page.Path)
		}
		paths[page.Path] = true
		for _, score := range append(append([]LighthouseScore(nil), page.Runs...), page.Median) {
			if score.Performance < 0 || score.Performance > 100 || score.Accessibility < 0 || score.Accessibility > 100 || score.BestPractices < 0 || score.BestPractices > 100 || score.SEO < 0 || score.SEO > 100 || score.CLS < 0 {
				return fmt.Errorf("Lighthouse page %q contains an invalid score", page.Path)
			}
		}
		if page.Median.Performance != medianInt(page.Runs, func(score LighthouseScore) int { return score.Performance }) ||
			page.Median.Accessibility != medianInt(page.Runs, func(score LighthouseScore) int { return score.Accessibility }) ||
			page.Median.BestPractices != medianInt(page.Runs, func(score LighthouseScore) int { return score.BestPractices }) ||
			page.Median.SEO != medianInt(page.Runs, func(score LighthouseScore) int { return score.SEO }) ||
			math.Abs(page.Median.CLS-medianFloat(page.Runs, func(score LighthouseScore) float64 { return score.CLS })) > 0.0001 {
			return fmt.Errorf("Lighthouse page %q median does not match its three runs", page.Path)
		}
	}
	for _, required := range []string{"/", "/docs", "/docs/getting-started/", "/demos/", "/capabilities/", "/performance/"} {
		if !paths[required] {
			return fmt.Errorf("Lighthouse page %q is required", required)
		}
	}
	if r.FeaturedPath == "" || !paths[r.FeaturedPath] {
		return fmt.Errorf("Lighthouse receipts must include the featured demo")
	}
	if r.GPU.Label != "RTX 5070 Ti desktop, not a mid-range laptop" || r.GPU.Browser == "" || r.GPU.Method == "" || len(r.GPU.Scenes) == 0 {
		return fmt.Errorf("GPU receipts require the reference label, browser, method, and scene timings")
	}
	scenePaths := make(map[string]bool, len(r.GPU.Scenes))
	for _, scene := range r.GPU.Scenes {
		if scene.Path == "" || scene.Title == "" || len(scene.Backends) != 2 {
			return fmt.Errorf("GPU scene %q must include both backends", scene.Path)
		}
		if scenePaths[scene.Path] {
			return fmt.Errorf("GPU scene %q appears more than once", scene.Path)
		}
		scenePaths[scene.Path] = true
		seen := map[string]bool{}
		for _, backend := range scene.Backends {
			if (backend.Backend != "WebGPU" && backend.Backend != "WebGL2") || seen[backend.Backend] || backend.FirstDrawMs <= 0 || backend.CadenceP50Ms <= 0 || backend.CadenceP95Ms <= 0 || backend.RafCPUP95Ms < 0 {
				return fmt.Errorf("GPU scene %q has incomplete %q timing", scene.Path, backend.Backend)
			}
			seen[backend.Backend] = true
		}
		if !seen["WebGPU"] || !seen["WebGL2"] {
			return fmt.Errorf("GPU scene %q must include WebGPU and WebGL2", scene.Path)
		}
	}
	for _, required := range []string{"/demos/showreel/", "/demos/checkers/", "/demos/beacon", "/demos/water", "/demos/scene3d/", "/demos/scene3d-bench", "/demos/html-surface/", "/demos/orrery/"} {
		if !scenePaths[required] {
			return fmt.Errorf("GPU timing for 3D demo %q is required", required)
		}
	}
	tabletopPath := ""
	for path := range scenePaths {
		if strings.TrimRight(path, "/") == "/demos/tabletop" {
			tabletopPath = path
			break
		}
	}
	if tabletopPath != "" {
		if r.FeaturedPath != tabletopPath {
			return fmt.Errorf("Tabletop is present and must be the featured demo")
		}
	} else if r.FeaturedPath != "/demos/water" {
		return fmt.Errorf("when Tabletop is unavailable, Water must be the featured demo")
	}
	if len(r.Bundles) == 0 {
		return fmt.Errorf("bundle size receipts are required")
	}
	kinds := map[string]bool{}
	for _, bundle := range r.Bundles {
		if bundle.Name == "" || bundle.Path == "" || bundle.RawBytes <= 0 || bundle.BrotliBytes <= 0 {
			return fmt.Errorf("bundle %q has invalid sizes", bundle.Name)
		}
		if bundle.Kind != "client-js" && bundle.Kind != "wasm" {
			return fmt.Errorf("bundle %q has unknown kind %q", bundle.Name, bundle.Kind)
		}
		kinds[bundle.Kind] = true
	}
	if !kinds["client-js"] || !kinds["wasm"] {
		return fmt.Errorf("both client JavaScript and WASM bundle sizes are required")
	}
	if r.Quickstart.Command == "" || r.Quickstart.Method == "" || r.Quickstart.ColdSec <= 0 || r.Quickstart.WarmSec <= 0 {
		return fmt.Errorf("cold and warm quickstart timings are required")
	}
	return nil
}

func medianInt(values []LighthouseScore, pick func(LighthouseScore) int) int {
	items := make([]int, len(values))
	for i, value := range values {
		items[i] = pick(value)
	}
	sort.Ints(items)
	return items[len(items)/2]
}

func medianFloat(values []LighthouseScore, pick func(LighthouseScore) float64) float64 {
	items := make([]float64, len(values))
	for i, value := range values {
		items[i] = pick(value)
	}
	sort.Float64s(items)
	return items[len(items)/2]
}
