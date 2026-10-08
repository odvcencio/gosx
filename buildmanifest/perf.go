package buildmanifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// PerfAssetUses describes versioned reachability evidence. A nil field in an
// older manifest means unknown reachability, even when compatibility validation
// succeeds. It never establishes a certified performance pass.
type PerfAssetUses struct {
	Version int            `json:"version"`
	Assets  []PerfAssetUse `json:"assets"`
}

// PerfAssetUse identifies a body and its declared dependencies. URLs are private
// origin-relative producer inputs; public reports retain logical IDs instead.
type PerfAssetUse struct {
	ID           string   `json:"id"`
	SHA256       string   `json:"sha256"`
	URL          string   `json:"url"`
	Owner        string   `json:"owner"`
	Kind         string   `json:"kind"`
	Phase        string   `json:"phase"`
	Condition    string   `json:"condition"`
	Dependencies []string `json:"dependencies"`
}

var perfPathPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*$`)
var perfHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var perfAppPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

// PerfAssetError contains only a fixed code and a location, never input values.
type PerfAssetError struct {
	Code    string `json:"code"`
	Pointer string `json:"pointer"`
}

func (e *PerfAssetError) Error() string { return e.Code + " at " + e.Pointer }
func perfError(pointer string) error {
	return &PerfAssetError{Code: "invalid-input", Pointer: "/perfAssetUses" + pointer}
}

func (p *PerfAssetUses) UnmarshalJSON(data []byte) error {
	fields, err := perfObject(data, []string{"version", "assets"})
	if err != nil {
		return err
	}
	var decoded PerfAssetUses
	if err := json.Unmarshal(fields["version"], &decoded.Version); err != nil {
		return perfError("/version")
	}
	var assets []json.RawMessage
	if err := json.Unmarshal(fields["assets"], &assets); err != nil || assets == nil || len(assets) > 4096 {
		return perfError("/assets")
	}
	decoded.Assets = make([]PerfAssetUse, len(assets))
	for i, raw := range assets {
		if err := json.Unmarshal(raw, &decoded.Assets[i]); err != nil {
			var input *PerfAssetError
			if errors.As(err, &input) {
				return perfError("/assets/" + strconv.Itoa(i) + strings.TrimPrefix(input.Pointer, "/perfAssetUses"))
			}
			return perfError("/assets/" + strconv.Itoa(i))
		}
	}
	if err := (&Manifest{PerfAssetUses: &decoded}).ValidatePerfAssetUses(); err != nil {
		return err
	}
	*p = decoded
	return nil
}

func (a *PerfAssetUse) UnmarshalJSON(data []byte) error {
	_, err := perfObject(data, []string{"id", "sha256", "url", "owner", "kind", "phase", "condition", "dependencies"})
	if err != nil {
		return err
	}
	type plain PerfAssetUse
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return perfError("")
	}
	if err := validatePerfAssetUse(PerfAssetUse(decoded), ""); err != nil {
		return err
	}
	*a = PerfAssetUse(decoded)
	return nil
}

func perfObject(data []byte, required []string) (map[string]json.RawMessage, error) {
	if len(data) > 2<<20 || !utf8.Valid(data) {
		return nil, perfError("")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, perfError("")
	}
	fields := make(map[string]json.RawMessage, len(required))
	for decoder.More() {
		token, err = decoder.Token()
		name, ok := token.(string)
		known := false
		for _, key := range required {
			known = known || key == name
		}
		if err != nil || !ok || !known {
			return nil, perfError("")
		}
		if _, exists := fields[name]; exists {
			return nil, perfError("/" + name)
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, perfError("/" + name)
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, perfError("/" + name)
		}
		fields[name] = raw
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, perfError("")
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, perfError("")
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return nil, perfError("/" + key)
		}
	}
	return fields, nil
}

func perfPath(value string) bool {
	if len(value) == 0 || len(value) > 240 || !perfPathPattern.MatchString(value) {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

func perfURL(value string) bool {
	if value == "/" {
		return true
	}
	if len(value) > 240 || !strings.HasPrefix(value, "/") {
		return false
	}
	return perfPath(strings.TrimSuffix(strings.TrimPrefix(value, "/"), "/"))
}

func perfEnum(value string, allowed ...string) bool {
	for _, choice := range allowed {
		if value == choice {
			return true
		}
	}
	return false
}

// ValidatePerfAssetUses validates a single graph without requiring new metadata
// on older manifests. Consumers must separately require PerfAssetUses != nil.
func (m *Manifest) ValidatePerfAssetUses() error {
	if m == nil {
		return perfError("")
	}
	p := m.PerfAssetUses
	if p == nil {
		return nil
	}
	if p.Version != 1 {
		return perfError("/version")
	}
	if p.Assets == nil || len(p.Assets) > 4096 {
		return perfError("/assets")
	}
	identities := make(map[string]PerfAssetUse)
	indexes := make(map[string]int)
	edges := make(map[string][]string)
	conditions := make(map[string]map[string]bool)
	for i, a := range p.Assets {
		location := "/assets/" + strconv.Itoa(i)
		if err := validatePerfAssetUse(a, location); err != nil {
			return err
		}
		if previous, ok := identities[a.ID]; ok {
			if previous.Owner != a.Owner || previous.SHA256 != a.SHA256 || previous.Kind != a.Kind {
				return perfError(location + "/id")
			}
		} else {
			identities[a.ID] = a
			indexes[a.ID] = i
		}
		if conditions[a.ID] == nil {
			conditions[a.ID] = make(map[string]bool)
		}
		conditions[a.ID][a.Condition] = true
		edges[a.ID] = append(edges[a.ID], a.Dependencies...)
	}
	for i, a := range p.Assets {
		for j, dep := range a.Dependencies {
			_, ok := identities[dep]
			onlyOther := len(conditions[dep]) == 1 && (a.Condition == "webgpu" && conditions[dep]["webgl"] || a.Condition == "webgl" && conditions[dep]["webgpu"])
			if !ok || onlyOther {
				return perfError("/assets/" + strconv.Itoa(i) + "/dependencies/" + strconv.Itoa(j))
			}
		}
	}
	state := make(map[string]uint8)
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return perfError("/assets/" + strconv.Itoa(indexes[id]) + "/dependencies")
		}
		if state[id] == 2 {
			return nil
		}
		state[id] = 1
		for _, dep := range edges[id] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	keys := make([]string, 0, len(identities))
	for id := range identities {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

// ValidatePerfAssetConsistency verifies global ID identity across app manifests.
// Identical framework IDs may be reused only with the same owner and raw SHA.
func ValidatePerfAssetConsistency(manifests ...*Manifest) error {
	seen := make(map[string]PerfAssetUse)
	for _, m := range manifests {
		if err := m.ValidatePerfAssetUses(); err != nil {
			return err
		}
		if m.PerfAssetUses == nil {
			continue
		}
		for i, a := range m.PerfAssetUses.Assets {
			if previous, ok := seen[a.ID]; ok && (previous.Owner != a.Owner || previous.SHA256 != a.SHA256 || previous.Kind != a.Kind) {
				return perfError("/assets/" + strconv.Itoa(i) + "/id")
			}
			seen[a.ID] = a
		}
	}
	return nil
}

func validatePerfAssetUse(a PerfAssetUse, location string) error {
	checks := []struct {
		field string
		valid bool
	}{
		{"id", perfPath(a.ID)}, {"sha256", perfHashPattern.MatchString(a.SHA256)}, {"url", perfURL(a.URL)},
		{"owner", perfEnum(a.Owner, "framework", "app")},
		{"kind", perfEnum(a.Kind, "html", "js", "wasm", "program", "css", "font", "image", "model", "video", "other")},
		{"phase", perfEnum(a.Phase, "critical", "startup", "after-ready", "dormant")},
		{"condition", perfEnum(a.Condition, "always", "webgpu", "webgl", "device-loss", "pipeline-recovery", "hls-required", "interaction")},
		{"dependencies", a.Dependencies != nil && len(a.Dependencies) <= 128},
	}
	for _, check := range checks {
		if !check.valid {
			return perfError(location + "/" + check.field)
		}
	}
	parts := strings.Split(a.ID, "/")
	if a.Owner == "app" && (len(parts) < 3 || parts[0] != "app" || !perfAppPattern.MatchString(parts[1])) {
		return perfError(location + "/id")
	}
	seen := make(map[string]bool)
	for j, dep := range a.Dependencies {
		if !perfPath(dep) || seen[dep] {
			return perfError(location + "/dependencies/" + strconv.Itoa(j))
		}
		seen[dep] = true
	}
	return nil
}
