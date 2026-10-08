package budget

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
)

type Goal struct {
	Metric string  `json:"metric"`
	Max    float64 `json:"max"`
	Unit   string  `json:"unit"`
}
type Mix struct {
	JSPPM                    int64 `json:"jsPPM"`
	WASMPPM                  int64 `json:"wasmPPM"`
	ProgramPPM               int64 `json:"programPPM"`
	OtherPPM                 int64 `json:"otherPPM"`
	WASMExpansionNumerator   int64 `json:"wasmExpansionNumerator"`
	WASMExpansionDenominator int64 `json:"wasmExpansionDenominator"`
}
type Workload struct {
	Islands     int64 `json:"islands"`
	Pipelines   int64 `json:"pipelines"`
	ShaderKB    int64 `json:"shaderKB"`
	Engines     int64 `json:"engines"`
	WASMModules int64 `json:"wasmModules"`
}
type Step struct {
	Name        string `json:"name"`
	Numerator   string `json:"numerator"`
	Denominator string `json:"denominator"`
	Unit        string `json:"unit"`
}
type Derivation struct {
	Kind                    string `json:"kind"`
	Status                  string `json:"status"`
	InputSHA256             string `json:"inputSHA256"`
	TotalBytes              int64  `json:"totalBytes"`
	AppCriticalReserveBytes int64  `json:"appCriticalReserveBytes"`
	MinAppBytes             int64  `json:"minAppBytes"`
	FrameworkBytes          int64  `json:"frameworkBytes"`
	Steps                   []Step `json:"steps"`
}
type Memory struct {
	JSSettledBytes    int64 `json:"jsSettledBytes"`
	JSPeakBytes       int64 `json:"jsPeakBytes"`
	WASMPeakBytes     int64 `json:"wasmPeakBytes"`
	GPUInitialBytes   int64 `json:"gpuInitialBytes"`
	GPUPeakBytes      int64 `json:"gpuPeakBytes"`
	CombinedPeakBytes int64 `json:"combinedPeakBytes"`
}
type WireEnvelope struct {
	Kind       string `json:"kind"`
	Compressor string `json:"compressor"`
	TotalBytes int64  `json:"totalBytes"`
}
type PageType struct {
	Network              string       `json:"network"`
	Goals                []Goal       `json:"goals"`
	PrimaryMetric        string       `json:"primaryMetric"`
	Mix                  Mix          `json:"mix"`
	AppReserveBytes      int64        `json:"appReserveBytes"`
	MinAppPPM            int64        `json:"minAppPPM"`
	Workload             Workload     `json:"workload"`
	Allocation           Derivation   `json:"allocation"`
	Memory               *Memory      `json:"memory"`
	RequiredPolicies     []string     `json:"requiredPolicies"`
	CoefficientSet       string       `json:"coefficientSet"`
	AfterReadyWorkload   Workload     `json:"afterReadyWorkload"`
	AfterReadyAllocation Derivation   `json:"afterReadyAllocation"`
	WireEnvelope         WireEnvelope `json:"wireEnvelope"`
	Backend              string       `json:"backend"`
}
type RouteRule struct {
	App           string   `json:"app"`
	RouteTemplate string   `json:"routeTemplate"`
	PageTypes     []string `json:"pageTypes"`
	Source        string   `json:"source"`
	Scenario      string   `json:"scenario"`
}
type Exception struct {
	ID             string `json:"id"`
	Scope          string `json:"scope"`
	Metric         string `json:"metric"`
	Extra          int64  `json:"extra"`
	Policy         string `json:"policy,omitempty"`
	ReasonCode     string `json:"reasonCode"`
	OwnerRole      string `json:"ownerRole"`
	ApprovedRole   string `json:"approvedRole"`
	Issue          int64  `json:"issue"`
	ApprovalReview int64  `json:"approvalReview"`
	Expires        string `json:"expires"`
}
type Guardrail struct {
	Key        string `json:"key"`
	Limit      int64  `json:"limit"`
	Unit       string `json:"unit"`
	Mode       string `json:"mode"`
	ReasonCode string `json:"reasonCode"`
}
type HubBudget struct {
	App                       string `json:"app"`
	Hub                       string `json:"hub"`
	Preset                    string `json:"preset"`
	ReferenceClients          int64  `json:"referenceClients"`
	MaxOutboundMessagesPerSec int64  `json:"maxOutboundMessagesPerSec"`
	MaxInboundMessagesPerSec  int64  `json:"maxInboundMessagesPerSec"`
	MaxPayloadBytes           int64  `json:"maxPayloadBytes"`
	NetworkSharePPM           int64  `json:"networkSharePPM"`
	Mode                      string `json:"mode"`
}
type Tripwire struct {
	MinBytes    int64 `json:"minBytes"`
	FractionPPM int64 `json:"fractionPPM"`
}

// File is the allocation configuration, not a measurement report.
type File struct {
	Schema       string              `json:"schema"`
	Profile      Ref                 `json:"profile"`
	Coefficients Ref                 `json:"coefficients"`
	Toolchain    Ref                 `json:"toolchain"`
	Fixtures     Ref                 `json:"fixtures"`
	Tripwire     Tripwire            `json:"tripwire"`
	PageTypes    map[string]PageType `json:"pageTypes"`
	Routes       []RouteRule         `json:"routes"`
	Exceptions   []Exception         `json:"exceptions"`
	Guardrails   []Guardrail         `json:"guardrails"`
	HubBudgets   []HubBudget         `json:"hubBudgets"`
}

// Load validates configuration and its hash-bound inputs under one project root.
// It does not certify the recorded allocation arithmetic.
func Load(path string, opts LoadOptions) (*File, error) {
	var f File
	root, err := loadInput(path, opts, "Budget", &f)
	if err != nil {
		return nil, err
	}
	var p Profile
	var c Coefficients
	var tc Toolchain
	for _, input := range []struct {
		ref        Ref
		definition string
		out        any
	}{{f.Profile, "Profile", &p}, {f.Coefficients, "Coefficients", &c}, {f.Toolchain, "Toolchain", &tc}} {
		data, err := readReference(root, input.ref, maxInputBytes)
		if err != nil {
			return nil, err
		}
		if err := decodeInput(data, input.definition, input.out); err != nil {
			return nil, err
		}
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	if err := tc.validate(root); err != nil {
		return nil, err
	}
	if c.ProfileSHA256 != f.Profile.SHA256 || c.Reference != p.Reference {
		return nil, errors.New("coefficient profile does not match")
	}
	fixtureBytes, err := readReference(root, f.Fixtures, maxInputBytes)
	if err != nil {
		return nil, err
	}
	var catalog json.RawMessage
	if err := decodeInput(fixtureBytes, "FixtureCatalog", &catalog); err != nil {
		return nil, err
	}
	var refs struct {
		InteractionContract Ref `json:"interactionContract"`
		Routes              []struct {
			App           string `json:"app"`
			RouteTemplate string `json:"routeTemplate"`
			SourcePath    string `json:"sourcePath"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(catalog, &refs); err != nil {
		return nil, errors.New("invalid fixture references")
	}
	if _, err := readReference(root, refs.InteractionContract, maxInputBytes); err != nil {
		return nil, err
	}
	registered := make(map[string]bool)
	for _, route := range refs.Routes {
		key := route.App + "|" + route.RouteTemplate
		if !safePath(route.SourcePath) || !validRoute(route.RouteTemplate) || registered[key] {
			return nil, errors.New("invalid fixture route")
		}
		if _, err := readWithin(root, filepath.Join(root, route.SourcePath), 16<<20); err != nil {
			return nil, err
		}
		registered[key] = true
	}
	for _, route := range f.Routes {
		if !registered[route.App+"|"+route.RouteTemplate] {
			return nil, errors.New("unregistered route template")
		}
	}
	if err := f.validate(p, c); err != nil {
		return nil, err
	}
	return &f, nil
}

func (f File) validate(p Profile, c Coefficients) error {
	sets := make(map[string]CoefficientSet)
	for _, s := range c.Sets {
		sets[s.ID] = s
	}
	for name, page := range f.PageTypes {
		if !knownPageType(name) {
			return errors.New("unregistered page type")
		}
		for _, backend := range []string{"webgpu", "webgl2"} {
			if strings.HasSuffix(name, "-"+backend) && page.Backend != backend {
				return errors.New("page backend does not match its variant")
			}
		}
		if page.Mix.JSPPM+page.Mix.WASMPPM+page.Mix.ProgramPPM+page.Mix.OtherPPM != 1000000 || page.MinAppPPM < 500000 {
			return errors.New("invalid byte mix or app minimum")
		}
		set, ok := sets[page.CoefficientSet]
		if !ok || set.Scenario != "hard-cold" || set.Backend != page.Backend {
			return errors.New("page requires a matching cold coefficient set")
		}
		goals := make(map[string]bool)
		for _, goal := range page.Goals {
			if goals[goal.Metric] || goal.Unit != metricUnit(goal.Metric) {
				return errors.New("invalid goal unit or duplicate metric")
			}
			goals[goal.Metric] = true
		}
		if !goals[page.PrimaryMetric] {
			return errors.New("primary goal is missing")
		}
		if page.Allocation.AppCriticalReserveBytes != page.AppReserveBytes {
			return errors.New("critical reserve does not match allocation")
		}
		if page.AfterReadyAllocation.AppCriticalReserveBytes != 0 {
			return errors.New("after-ready work cannot reserve critical bytes")
		}
		for _, entry := range set.Entries {
			if entry.Status == "unused" && (coefficientUsed(entry.Name, page.Mix, page.Workload) || coefficientUsed(entry.Name, page.Mix, page.AfterReadyWorkload)) {
				return errors.New("unused coefficient has nonzero workload")
			}
		}
		for _, d := range []Derivation{page.Allocation, page.AfterReadyAllocation} {
			if d.FrameworkBytes+d.MinAppBytes+d.AppCriticalReserveBytes != d.TotalBytes {
				return errors.New("allocation totals do not reconcile")
			}
			if name == "static" && d.FrameworkBytes != 0 {
				return errors.New("static allocation must leave all bytes to apps")
			}
			pool := d.TotalBytes - d.AppCriticalReserveBytes
			minimum := pool/1000000*page.MinAppPPM + (pool%1000000*page.MinAppPPM+999999)/1000000
			minimum = (minimum + p.QuantumBytes - 1) / p.QuantumBytes * p.QuantumBytes
			if d.MinAppBytes < minimum {
				return errors.New("allocation does not guarantee the app minimum")
			}
			if d.Status != "illustrative" {
				if d.Status == "phone-measured" {
					return errors.New("phone certification is unavailable")
				}
				if d.Status == "phone-measured" && p.Reference != "phone-4gb" || d.Status == "proxy-measured" && p.Reference != "desktop-cpu-proxy" {
					return errors.New("allocation reference does not match evidence")
				}
				for _, e := range set.Entries {
					if e.Status != "measured" && e.Status != "unused" {
						return errors.New("allocation has unsupported measured status")
					}
				}
			}
		}
	}
	seen := make(map[string]bool)
	for _, route := range f.Routes {
		key := route.App + "|" + route.RouteTemplate
		if seen[key] || !validRoute(route.RouteTemplate) {
			return errors.New("invalid or duplicate route rule")
		}
		seen[key] = true
		for _, name := range route.PageTypes {
			if _, ok := f.PageTypes[name]; !ok {
				return errors.New("route names an unconfigured page type")
			}
		}
	}
	seen = make(map[string]bool)
	for _, e := range f.Exceptions {
		if seen[e.ID] || e.Metric == "frameworkBytes" {
			return errors.New("duplicate or forbidden framework-share exception")
		}
		seen[e.ID] = true
	}
	seen = make(map[string]bool)
	want := map[string]Guardrail{
		"criticalRequests":         {Limit: 10, Unit: "count", Mode: "gate", ReasonCode: "guardrail"},
		"startupRequests":          {Limit: 20, Unit: "count", Mode: "gate", ReasonCode: "guardrail"},
		"afterReadyRequests":       {Limit: 24, Unit: "count", Mode: "gate", ReasonCode: "guardrail"},
		"inlineAppExecutableBytes": {Limit: 1024, Unit: "B", Mode: "gate", ReasonCode: "guardrail"},
		"htmlFirstFlightBytes":     {Limit: 14600, Unit: "B", Mode: "target", ReasonCode: "initial-cwnd"},
		"fontCount":                {Limit: 2, Unit: "count", Mode: "target", ReasonCode: "first-paint"},
	}
	for _, g := range f.Guardrails {
		w := want[g.Key]
		w.Key = g.Key
		if seen[g.Key] || g != w {
			return errors.New("invalid or duplicate guardrail")
		}
		seen[g.Key] = true
	}
	return nil
}

func metricUnit(metric string) string {
	switch metric {
	case "cls", "dropped_frame_rate":
		return "ratio"
	case "frozen_frames", "long_animation_frames", "long_tasks", "pipelines_after_fif", "sync_pipelines_after_fif":
		return "count"
	}
	if strings.Contains(metric, "heap") || strings.Contains(metric, "memory") || strings.HasPrefix(metric, "gpu_") || metric == "combined_tracked_peak" {
		return "B"
	}
	return "ms"
}

func knownPageType(name string) bool {
	for _, suffix := range []string{"-webgpu", "-webgl2"} {
		if strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
			break
		}
	}
	switch name {
	case "static", "enhanced", "island", "engine/js", "engine/shared", "go-wasm", "video", "scene3d/shared", "game/shared", "preview", "scene3d/js", "game/js":
		return true
	}
	return false
}
func validRoute(route string) bool {
	for _, part := range strings.Split(route, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

func coefficientUsed(name string, mix Mix, w Workload) bool {
	switch name {
	case "jsMicrosPerKB":
		return mix.JSPPM > 0
	case "wasmCompileMicrosPerRawKB", "wasmOverlapPPM":
		return mix.WASMPPM > 0
	case "programDecodeMicrosPerKB":
		return mix.ProgramPPM > 0
	case "wasmInstantiateMicros":
		return w.WASMModules > 0
	case "engineStartMicros":
		return w.Engines > 0
	case "pipelineCreateMicros":
		return w.Pipelines > 0
	case "shaderCompileMicrosPerKB":
		return w.ShaderKB > 0
	case "hydrationMicrosPerIsland":
		return w.Islands > 0
	default:
		return true
	}
}
func inputDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
