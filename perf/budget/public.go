package budget

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// PublicValidator binds public identifiers to a trusted fixture catalog and the
// tracked repository sources. Its inputs are native options, never report fields.
type PublicValidator struct {
	sources map[string]bool
	routes  map[string]map[string]bool
	apps    map[string]bool
	types   map[string]bool
	assets  map[string]string
	hubs    map[string]bool
}

// NewPublicValidator snapshots catalog and Git membership. Hub budgets must come
// from the trusted configuration, not from the artifact being validated.
func NewPublicValidator(catalogPath string, opts LoadOptions, hubs []HubBudget) (*PublicValidator, error) {
	fail := func(p string) (*PublicValidator, error) {
		return nil, inputReference(invalidInput(p), "public-catalog", "")
	}
	root, err := inputRoot(catalogPath, opts)
	if err != nil {
		return fail("")
	}
	out := &publicBoundedBuffer{}
	cmd := exec.Command("git", "-C", root, "ls-files", "-z", "--")
	cmd.Stdout = out
	if cmd.Run() != nil {
		return fail("/sources")
	}
	v := &PublicValidator{sources: map[string]bool{}, routes: map[string]map[string]bool{}, apps: map[string]bool{}, types: map[string]bool{}, assets: map[string]string{}, hubs: map[string]bool{}}
	for _, source := range strings.Split(out.String(), "\x00") {
		if safePath(source) {
			v.sources[source] = true
		}
	}
	absolute, err := filepath.Abs(catalogPath)
	if err != nil {
		return fail("")
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return fail("/catalog")
	}
	rel, err := filepath.Rel(root, absolute)
	if err != nil || !v.sources[filepath.ToSlash(rel)] {
		return fail("/catalog")
	}
	data, err := readWithin(root, absolute, maxInputBytes)
	if err != nil {
		return fail("/catalog")
	}
	var catalog struct {
		Schema  string `json:"schema"`
		Version int64  `json:"version"`
		Routes  []struct {
			App              string          `json:"app"`
			RouteTemplate    string          `json:"routeTemplate"`
			SourcePath       string          `json:"sourcePath"`
			PageTypes        []string        `json:"pageTypes"`
			Capabilities     json.RawMessage `json:"capabilities"`
			StartupCounts    Workload        `json:"startupCounts"`
			CriticalAssetIDs []string        `json:"criticalAssetIDs"`
			InputSequenceID  string          `json:"inputSequenceID"`
			GPUEstimate      json.RawMessage `json:"gpuEstimate"`
		} `json:"routes"`
		AssetRules []struct {
			ID           string   `json:"id"`
			Owner        string   `json:"owner"`
			Kind         string   `json:"kind"`
			Phase        string   `json:"phase"`
			Condition    string   `json:"condition"`
			Dependencies []string `json:"dependencies"`
		} `json:"assetRules"`
		InteractionContract Ref `json:"interactionContract"`
	}
	if err := decodeInput(data, "FixtureCatalog", &catalog); err != nil {
		return nil, err
	}
	for i, route := range catalog.Routes {
		p := "/routes/" + strconv.Itoa(i)
		key := route.App + "|" + route.RouteTemplate
		if !v.sources[route.SourcePath] || !validRoute(route.RouteTemplate) || v.routes[key] != nil {
			return fail(p)
		}
		v.apps[route.App] = true
		v.routes[key] = map[string]bool{}
		for _, name := range route.PageTypes {
			if !knownPageType(name) {
				return fail(p + "/pageTypes")
			}
			v.routes[key][name] = true
			v.types[name] = true
		}
	}
	for i, asset := range catalog.AssetRules {
		if v.assets[asset.ID] != "" {
			return fail("/assetRules/" + strconv.Itoa(i))
		}
		v.assets[asset.ID] = asset.Owner
	}
	// Whole runtime identities come from tracked emitted files, not source sizes.
	for _, name := range []string{"bootstrap.js", "bootstrap-lite.js", "bootstrap-loader.js", "wasm_exec.js", "patch.js", "relay.js", "hls.min.js"} {
		if v.sources["client/js/"+name] {
			v.assets["framework/runtime/"+name] = "framework"
		}
	}
	if v.sources["client/js/bootstrap-src/chunks.json"] {
		data, err := readWithin(root, filepath.Join(root, "client/js/bootstrap-src/chunks.json"), maxInputBytes)
		var chunks struct {
			Chunks []struct {
				Name string `json:"name"`
			} `json:"chunks"`
		}
		if err != nil || json.Unmarshal(data, &chunks) != nil {
			return fail("/assets")
		}
		for _, chunk := range chunks.Chunks {
			if safePath(chunk.Name) && v.sources["client/js/"+chunk.Name] {
				v.assets["framework/runtime/"+chunk.Name] = "framework"
			}
		}
	}
	if v.sources["client/runtime/host/navigation.ts"] {
		v.assets["framework/runtime/navigation.js"] = "framework"
	}
	// These loaders are emitted from pinned toolchains by the tracked producer.
	if v.sources["cmd/gosx/build.go"] {
		v.assets["framework/runtime/wasm_exec.js"] = "framework"
		v.assets["framework/runtime/standard-go-wasm_exec.js"] = "framework"
	}
	if v.sources["client/runtime/wasm/abi.go"] {
		for _, variant := range []string{"core", "engine", "collab", "full", "islands"} {
			v.assets["framework/runtime/"+variant+".wasm"] = "framework"
		}
	}
	for i, hub := range hubs {
		if !v.apps[hub.App] || validateInput(hub.Hub, inputDefinitions["ID"]) != nil || v.hubs[hub.App+"|"+hub.Hub] {
			return fail("/hubs/" + strconv.Itoa(i))
		}
		v.hubs[hub.App+"|"+hub.Hub] = true
	}
	return v, nil
}

type publicBoundedBuffer struct{ bytes.Buffer }

func (b *publicBoundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxInputBytes {
		return 0, invalidInput("")
	}
	return b.Buffer.Write(p)
}

func defaultPublicValidator() (*PublicValidator, error) {
	root, err := inputRoot("go.mod", LoadOptions{})
	if err != nil {
		return nil, inputReference(err, "public-catalog", "")
	}
	catalog := filepath.Join(root, "perf/fixtures/catalog.v1.json")
	if _, err := os.Stat(catalog); os.IsNotExist(err) {
		catalog = filepath.Join(root, "perf/budget/testdata/catalog.v1.json")
	}
	return NewPublicValidator(catalog, LoadOptions{RootDir: root}, nil)
}

// ValidatePublic refuses the complete artifact unless every record is closed and
// all identifiers belong to the registered catalog or tracked source inventory.
func ValidatePublic(r io.Reader, format string) error {
	v, err := defaultPublicValidator()
	if err != nil {
		return err
	}
	return v.Validate(r, format)
}

func (v *PublicValidator) validateMembership(record any) error {
	fail := func(p string) error { return inputReference(invalidInput(p), "public-record", "") }
	route := func(app, template, name, p string) error {
		types := v.routes[app+"|"+template]
		if types == nil || name != "" && !types[name] {
			return fail(p)
		}
		return nil
	}
	if v == nil {
		return fail("")
	}
	switch r := record.(type) {
	case *Report:
		for i, row := range r.Rows {
			p := "/rows/" + strconv.Itoa(i)
			if err := route(row.App, row.RouteTemplate, row.PageType, p); err != nil {
				return err
			}
			for j, policy := range row.Policies {
				props := inputDefinitions["PageType"].(map[string]any)["properties"].(map[string]any)
				item := props["requiredPolicies"].(map[string]any)["items"]
				if validateInput(policy.Name, item) != nil {
					return fail(p + "/policies/" + strconv.Itoa(j) + "/name")
				}
			}
		}
		for i, asset := range r.Assets {
			p := "/assets/" + strconv.Itoa(i)
			if !v.apps[asset.App] || v.assets[asset.ID] != asset.Owner || asset.Owner == "app" && !strings.HasPrefix(asset.ID, "app/"+asset.App+"/") {
				return fail(p + "/id")
			}
			for j, source := range asset.ChangedSources {
				if !v.sources[source] {
					return fail(p + "/changedSources/" + strconv.Itoa(j))
				}
			}
			for j, id := range asset.Dependencies {
				if v.assets[id] == "" {
					return fail(p + "/dependencies/" + strconv.Itoa(j))
				}
			}
		}
		for i, ack := range r.Acknowledgments {
			p := "/acknowledgments/" + strconv.Itoa(i)
			if ack.Kind == "timing" {
				parts := strings.Split(ack.Scope, "|")
				if len(parts) != 6 || parts[5] != ack.Metric {
					return fail(p + "/scope")
				}
				c := Cell{App: parts[0], RouteTemplate: parts[1], PageType: parts[2], Scenario: parts[3], Backend: parts[4], Metric: parts[5], Unit: "ms"}
				if validateCellSchema(c) != nil || route(c.App, c.RouteTemplate, c.PageType, p) != nil {
					return fail(p + "/scope")
				}
			} else {
				kind, target, ok := strings.Cut(ack.Scope, ":")
				switch kind {
				case "asset":
					ok = ok && v.assets[target] != ""
				case "type":
					ok = ok && v.types[target]
				case "route":
					app, template, parsed := strings.Cut(target, ":")
					ok = ok && parsed && v.routes[app+"|"+template] != nil
				default:
					ok = false
				}
				if !ok {
					return fail(p + "/scope")
				}
				switch ack.Metric {
				case "raw", "gzip", "brotli", "totalBytes", "frameworkBytes":
				default:
					return fail(p + "/metric")
				}
			}
		}
	case *SeriesPoint:
		return route(r.Cell.App, r.Cell.RouteTemplate, r.Cell.PageType, "/cell")
	case *PairReport:
		for i, row := range r.Cells {
			if err := route(row.Cell.App, row.Cell.RouteTemplate, row.Cell.PageType, "/cells/"+strconv.Itoa(i)+"/cell"); err != nil {
				return err
			}
		}
	case *FieldSnapshot:
		for i, vital := range r.Vitals {
			if err := route(vital.App, vital.RouteTemplate, "", "/vitals/"+strconv.Itoa(i)); err != nil {
				return err
			}
		}
		for i, hub := range r.Hubs {
			if !v.hubs[hub.App+"|"+hub.Hub] {
				return fail("/hubs/" + strconv.Itoa(i))
			}
		}
	case *RunStatus:
	default:
		return fail("/schema")
	}
	return nil
}

func validateCellSchema(cell Cell) error {
	data, err := json.Marshal(cell)
	if err != nil {
		return invalidInput("/cell")
	}
	var decoded Cell
	if err := decodeInput(data, "Cell", &decoded); err != nil {
		return err
	}
	probe := &SeriesPoint{Cell: cell, At: "2000-01-01T00:00:00Z", N: 1, Samples: []float64{0}}
	return validateRecordDomains(probe)
}

// Validate supports a single JSON root, newline-delimited roots, or the exact
// Markdown representation emitted by WriteMarkdown. Arbitrary prose is refused.
func (v *PublicValidator) Validate(r io.Reader, format string) error {
	record := func(data []byte) error {
		decoded, err := DecodeRecord(bytes.NewReader(data))
		if err != nil {
			return err
		}
		return v.validateMembership(decoded)
	}
	switch format {
	case "json":
		data, err := readPublicArtifact(r, maxInputBytes)
		if err != nil {
			return err
		}
		return record(data)
	case "jsonl":
		limited := &io.LimitedReader{R: r, N: 32*maxInputBytes + 1}
		scanner := bufio.NewScanner(limited)
		scanner.Buffer(make([]byte, 4096), maxInputBytes+1)
		n := 0
		for scanner.Scan() {
			data := scanner.Bytes()
			n++
			if len(data) == 0 || n > 4096 {
				return inputReference(invalidInput(""), "public-record", "")
			}
			if err := record(data); err != nil {
				return err
			}
		}
		// Exhausting the raw-byte allowance can hide an unvalidated suffix
		// behind the limited reader's EOF, even when scanning succeeds.
		if scanner.Err() != nil || limited.N == 0 || n == 0 {
			return inputReference(invalidInput(""), "public-record", "")
		}
		return nil
	case "markdown":
		data, err := readPublicArtifact(r, 2*maxInputBytes)
		if err != nil {
			return err
		}
		marker := []byte("\n```json\n")
		start := bytes.Index(data, marker)
		if start < 0 {
			return inputReference(invalidInput(""), "public-record", "")
		}
		tail := data[start+len(marker):]
		end := bytes.Index(tail, []byte("\n```\n"))
		if end < 0 {
			return inputReference(invalidInput(""), "public-record", "")
		}
		decoded, err := DecodeRecord(bytes.NewReader(tail[:end]))
		if err != nil {
			return err
		}
		report, ok := decoded.(*Report)
		if !ok {
			return inputReference(invalidInput("/schema"), "public-record", "")
		}
		if err := v.validateMembership(report); err != nil {
			return err
		}
		expected, err := renderPublicMarkdown(*report)
		if err != nil || !bytes.Equal(data, expected) {
			return inputReference(invalidInput(""), "public-record", "")
		}
		return nil
	default:
		return inputReference(invalidInput(""), "public-format", "")
	}
}

func readPublicArtifact(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, inputReference(invalidInput(""), "public-record", "")
	}
	return data, nil
}
