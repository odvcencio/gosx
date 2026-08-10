// Package hydrate defines the hydration manifest and island metadata
// that connects server-rendered HTML to client-side WASM islands.
package hydrate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"m31labs.dev/gosx/assetpipe"
	"m31labs.dev/gosx/controller"
	"m31labs.dev/gosx/engine"
)

// Manifest describes all islands and engines on a page.
type Manifest struct {
	// Version of the manifest format.
	Version string `json:"version"`

	// Islands lists every island instance on the page.
	Islands []IslandEntry `json:"islands"`

	// ComputeIslands lists headless shared-runtime islands on the page.
	ComputeIslands []ComputeIslandEntry `json:"computeIslands,omitempty"`

	// Engines lists every engine instance on the page.
	Engines []EngineEntry `json:"engines,omitempty"`

	// SelfDescribingSurfaces lists no-code browser surfaces already described
	// by server-rendered DOM attributes such as data-gosx-surface-kind.
	SelfDescribingSurfaces []SelfDescribingSurfaceEntry `json:"selfDescribingSurfaces,omitempty"`

	// Hubs lists realtime hub connections the client should establish.
	Hubs []HubEntry `json:"hubs,omitempty"`

	// Controllers lists headless declarative browser controllers for the page.
	Controllers []ControllerEntry `json:"controllers,omitempty"`

	// ClientIdentity describes optional browser-owned client identity state.
	ClientIdentity *ClientIdentityConfig `json:"clientIdentity,omitempty"`

	// Bundles maps bundle IDs to WASM asset paths.
	Bundles map[string]BundleRef `json:"bundles"`

	// Runtime points to the shared island WASM runtime.
	Runtime RuntimeRef `json:"runtime"`

	// TextureVariants maps one source asset path onto the built encodings of
	// that texture. The client reads it to swap an authored image URI for a
	// block-compressed file the live device can upload.
	//
	// The map carries built files only. A planned variant names work the
	// pipeline did not do, so publishing one would produce a 404 in the
	// browser. SetTextureVariants fills the map from a variant manifest, which
	// assetpipe.BuildVariantManifest already filtered on the built state.
	TextureVariants map[string][]ManifestVariantRef `json:"textureVariants,omitempty"`
}

// SelfDescribingSurfaceEntry describes a no-code browser surface kind.
//
// These entries activate a bootstrap feature and runtime without creating a
// generic engine entry. The DOM node itself carries the mount contract.
type SelfDescribingSurfaceEntry struct {
	Kind                 string   `json:"kind"`
	Feature              string   `json:"feature,omitempty"`
	Runtime              string   `json:"runtime,omitempty"`
	Count                int      `json:"count,omitempty"`
	Capabilities         []string `json:"capabilities,omitempty"`
	RequiredCapabilities []string `json:"requiredCapabilities,omitempty"`
}

// SelfDescribingSurfaceFeature names a bootstrap feature that can own
// self-describing DOM surfaces.
type SelfDescribingSurfaceFeature string

const (
	SelfDescribingSurfaceFeatureEngines     SelfDescribingSurfaceFeature = "engines"
	SelfDescribingSurfaceFeatureHubs        SelfDescribingSurfaceFeature = "hubs"
	SelfDescribingSurfaceFeatureControllers SelfDescribingSurfaceFeature = "controllers"
	SelfDescribingSurfaceFeatureIslands     SelfDescribingSurfaceFeature = "islands"
	SelfDescribingSurfaceFeatureScene3D     SelfDescribingSurfaceFeature = "scene3d"
)

// SelfDescribingSurfaceRuntime names the runtime lane a surface needs.
type SelfDescribingSurfaceRuntime string

const (
	SelfDescribingSurfaceRuntimeShared SelfDescribingSurfaceRuntime = "shared"
)

// ManifestVariantRef is one built asset variant a client may choose.
//
// The four fields are exactly what a selector needs and nothing more: the URI
// to fetch, the delivery tier, the byte cost that breaks a tier tie, and the
// capability tokens the consumer must hold. A client that lacks one required
// token must keep the source URI.
type ManifestVariantRef struct {
	URI                  string   `json:"uri"`
	Quality              string   `json:"quality,omitempty"`
	Bytes                int64    `json:"bytes,omitempty"`
	RequiredCapabilities []string `json:"requiredCapabilities,omitempty"`
}

// EngineEntry describes a single engine instance.
// Engines are arbitrary client compute modules — separate from islands.
type EngineEntry struct {
	// ID is the DOM anchor ID (e.g., "gosx-engine-0").
	ID string `json:"id"`

	// Component is the engine function name.
	Component string `json:"component"`

	// Kind is "worker" (background compute), "surface" (owns a DOM mount
	// point), or "video" (framework-owned managed video mount).
	Kind string `json:"kind"`

	// ProgramRef is the URL path to the engine's WASM bundle.
	ProgramRef string `json:"programRef"`

	// MountID is the DOM element ID the engine should attach to.
	MountID string `json:"mountId,omitempty"`

	// Runtime selects the GoSX client runtime for this engine.
	Runtime string `json:"runtime,omitempty"`

	// Props is the JSON-serialized props snapshot.
	Props json.RawMessage `json:"props"`

	// Capabilities declares what browser APIs the engine can use.
	Capabilities []string `json:"capabilities,omitempty"`

	// RequiredCapabilities declares browser APIs that must be present before
	// the client runtime mounts the engine.
	RequiredCapabilities []string `json:"requiredCapabilities,omitempty"`

	// PixelSurface carries the managed pixel framebuffer config when the engine
	// declares CapPixelSurface. The client runtime uses this to create the
	// canvas, scaling pipeline, and frame buffer interface.
	PixelSurface *engine.PixelSurfaceConfig `json:"pixelSurface,omitempty"`
}

// HubEntry describes a realtime hub connection for the page.
type HubEntry struct {
	// ID is the stable manifest identifier for this connection.
	ID string `json:"id"`

	// Name is the hub name.
	Name string `json:"name"`

	// Path is the WebSocket endpoint path or absolute ws/wss URL.
	Path string `json:"path"`

	// Bindings map hub events to shared island signals.
	Bindings []HubBinding `json:"bindings,omitempty"`

	// Input describes optional browser input forwarding owned by the GoSX
	// bootstrap rather than page-authored JavaScript.
	Input *HubInputConfig `json:"input,omitempty"`
}

// ControllerEntry describes one bootstrap-owned headless controller instance.
type ControllerEntry struct {
	ID     string            `json:"id"`
	Config controller.Config `json:"config"`
}

// HubBinding maps an inbound hub event to a shared signal, a soft page
// refresh, or both. Direction, throttle, and debounce also describe outbound
// signal bindings.
type HubBinding struct {
	Event      string `json:"event"`
	Signal     string `json:"signal,omitempty"`
	Direction  string `json:"direction,omitempty"`
	ThrottleMS int    `json:"throttleMs,omitempty"`
	DebounceMS int    `json:"debounceMs,omitempty"`
	// Refresh forces a same-URL soft navigation after a matching inbound event.
	Refresh bool `json:"refresh,omitempty"`
	// RefreshDebounceMS joins the hub connection's refresh burst and rearms its
	// single timer. Zero still coalesces matching events until the current task ends.
	RefreshDebounceMS int `json:"refreshDebounceMs,omitempty"`
	// RefreshPreserveScroll defaults to true; false wins within a pending burst.
	RefreshPreserveScroll *bool `json:"refreshPreserveScroll,omitempty"`
}

// HubInputConfig lets the browser bootstrap forward bounded, page-declared
// input state to a realtime hub.
type HubInputConfig struct {
	// Mode selects the input translator. Empty uses a raw browser profile;
	// "fighting" maps keyboard, touch controls, and gamepads to fight inputs.
	Mode string `json:"mode,omitempty"`

	// Event is the hub event used for input snapshots. Defaults to "input".
	Event string `json:"event,omitempty"`

	// ReadyEvent is sent once the socket opens. Defaults to "ready".
	ReadyEvent string `json:"readyEvent,omitempty"`

	// TrainingEvent is used for bootstrap-owned training shortcuts.
	TrainingEvent string `json:"trainingEvent,omitempty"`

	// Signal receives local input/cue state for islands.
	Signal string `json:"signal,omitempty"`

	// TrainingSignal receives local training overlay state for islands.
	TrainingSignal string `json:"trainingSignal,omitempty"`

	// TouchRoot limits data-dir/data-btn touch controls to a page region.
	TouchRoot string `json:"touchRoot,omitempty"`

	// Player is the local player number for online play.
	Player int `json:"player,omitempty"`

	// Local mirrors both players from one browser using pad 0/1 plus keyboard.
	Local bool `json:"local,omitempty"`

	// Spectator joins the realtime hub and sends ready/training events without
	// forwarding player input. This is useful for CPU-vs-CPU showcases.
	Spectator bool `json:"spectator,omitempty"`

	// SlotToken is included in ready/input/training payloads when present.
	SlotToken string `json:"slotToken,omitempty"`

	// SendEveryMS clamps the input send cadence. Defaults to 16ms.
	SendEveryMS int `json:"sendEveryMs,omitempty"`

	// Root scopes page-level input controllers.
	Root string `json:"root,omitempty"`

	// Username is included in lobby/join payloads for menu controllers.
	Username string `json:"username,omitempty"`

	// FightPath is the navigation target after a match is locked.
	FightPath string `json:"fightPath,omitempty"`

	// CPUEndpoint starts a solo match for arcade-select controllers.
	CPUEndpoint string `json:"cpuEndpoint,omitempty"`

	// LocalEndpoint starts a same-browser versus match.
	LocalEndpoint string `json:"localEndpoint,omitempty"`

	// FightCurrentEndpoint stores the current match before navigation.
	FightCurrentEndpoint string `json:"fightCurrentEndpoint,omitempty"`

	// MinLocalGamepads is required before same-browser versus can start.
	MinLocalGamepads int `json:"minLocalGamepads,omitempty"`

	// AttractSignal receives attract-mode state.
	AttractSignal string `json:"attractSignal,omitempty"`

	// LobbySignal receives lobby status snapshots.
	LobbySignal string `json:"lobbySignal,omitempty"`

	// VSSignal receives pre-fight transition state.
	VSSignal string `json:"vsSignal,omitempty"`
}

// ClientIdentityConfig lets the GoSX bootstrap maintain a stable anonymous
// browser identity without app-authored JavaScript.
type ClientIdentityConfig struct {
	StorageKey        string   `json:"storageKey,omitempty"`
	CookieName        string   `json:"cookieName,omitempty"`
	LegacyCookieNames []string `json:"legacyCookieNames,omitempty"`
	HeaderName        string   `json:"headerName,omitempty"`
	GlobalName        string   `json:"globalName,omitempty"`
	Prefix            string   `json:"prefix,omitempty"`
	MaxAgeSeconds     int      `json:"maxAgeSeconds,omitempty"`
	SameSite          string   `json:"sameSite,omitempty"`
}

// RuntimeRef points to the shared WASM runtime.
type RuntimeRef struct {
	// Path to the shared runtime .wasm file.
	Path string `json:"path"`

	// Hash for cache busting.
	Hash string `json:"hash,omitempty"`

	// Size in bytes (compressed).
	Size int64 `json:"size,omitempty"`
}

// IslandEntry describes a single island instance in the rendered HTML.
type IslandEntry struct {
	// ID is the stable DOM anchor ID (e.g., "gosx-island-0").
	ID string `json:"id"`

	// Component is the fully qualified component name.
	Component string `json:"component"`

	// BundleID references an entry in Manifest.Bundles.
	BundleID string `json:"bundleId"`

	// Props is the JSON-serialized props snapshot.
	Props json.RawMessage `json:"props"`

	// Events lists the event bindings for this island.
	Events []EventSlot `json:"events"`

	// Static is true if the island has no dynamic content and can skip hydration.
	Static bool `json:"static,omitempty"`

	// Checksum is a hash of the component source for cache invalidation.
	Checksum string `json:"checksum,omitempty"`

	// ProgramRef is the URL path to the IslandProgram asset.
	ProgramRef string `json:"programRef,omitempty"`

	// ProgramFormat is "json" (dev) or "bin" (prod).
	ProgramFormat string `json:"programFormat,omitempty"`

	// ProgramHash is a content hash for cache busting.
	ProgramHash string `json:"programHash,omitempty"`
}

// ComputeIslandEntry describes a headless shared-runtime island.
// Compute islands use the island VM and shared-signal bridge without owning a
// DOM root or an engine factory.
type ComputeIslandEntry struct {
	// ID is the stable compute island ID (e.g., "gosx-compute-0").
	ID string `json:"id"`

	// Component is the fully qualified component name.
	Component string `json:"component"`

	// BundleID references an entry in Manifest.Bundles.
	BundleID string `json:"bundleId,omitempty"`

	// Props is the JSON-serialized props snapshot.
	Props json.RawMessage `json:"props"`

	// Capabilities declares browser APIs the compute island can use.
	Capabilities []string `json:"capabilities,omitempty"`

	// RequiredCapabilities hard-gates browser APIs before hydration.
	RequiredCapabilities []string `json:"requiredCapabilities,omitempty"`

	// ProgramRef is the URL path to the IslandProgram asset.
	ProgramRef string `json:"programRef,omitempty"`

	// ProgramFormat is "json" (dev) or "bin" (prod).
	ProgramFormat string `json:"programFormat,omitempty"`

	// ProgramHash is a content hash for cache busting.
	ProgramHash string `json:"programHash,omitempty"`
}

// BundleRef points to a compiled WASM bundle.
type BundleRef struct {
	// Path is the URL path to the .wasm file.
	Path string `json:"path"`

	// Size is the compressed size in bytes.
	Size int64 `json:"size,omitempty"`

	// Hash is a content hash for cache busting.
	Hash string `json:"hash,omitempty"`
}

// EventSlot describes a single event binding within an island.
type EventSlot struct {
	// SlotID is a stable identifier for this handler.
	SlotID string `json:"slotId"`

	// EventType is the DOM event name (click, input, submit, etc.).
	EventType string `json:"eventType"`

	// TargetSelector identifies the DOM element within the island.
	TargetSelector string `json:"targetSelector,omitempty"`

	// HandlerName is the Go function name in the WASM bundle.
	HandlerName string `json:"handlerName"`

	// ServerAction is true if this event triggers a server round-trip.
	ServerAction bool `json:"serverAction,omitempty"`
}

// Marshal serializes the manifest to JSON.
func (m *Manifest) Marshal() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// Unmarshal deserializes a manifest from JSON.
func Unmarshal(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// NewManifest creates an empty manifest.
func NewManifest() *Manifest {
	return &Manifest{
		Version: "0.1.0",
		Bundles: make(map[string]BundleRef),
	}
}

// AddIsland adds an island entry and returns the assigned ID.
func (m *Manifest) AddIsland(component string, bundleID string, props any) (string, error) {
	propsJSON, err := json.Marshal(props)
	if err != nil {
		return "", err
	}
	id := islandID(len(m.Islands))
	entry := IslandEntry{
		ID:        id,
		Component: component,
		BundleID:  bundleID,
		Props:     propsJSON,
		Events:    []EventSlot{},
	}
	m.Islands = append(m.Islands, entry)
	return id, nil
}

// AddComputeIsland adds a headless shared-runtime island entry and returns its
// assigned ID.
func (m *Manifest) AddComputeIsland(component string, bundleID string, props any, capabilities, requiredCapabilities []string) (string, error) {
	propsJSON, err := json.Marshal(props)
	if err != nil {
		return "", err
	}
	id := computeIslandID(len(m.ComputeIslands))
	entry := ComputeIslandEntry{
		ID:                   id,
		Component:            component,
		BundleID:             bundleID,
		Props:                propsJSON,
		Capabilities:         capabilities,
		RequiredCapabilities: requiredCapabilities,
	}
	m.ComputeIslands = append(m.ComputeIslands, entry)
	return id, nil
}

// AddEngine adds an engine entry and returns the assigned ID.
func (m *Manifest) AddEngine(component, kind, programRef string, props any, capabilities []string) (string, error) {
	return m.AddEngineWithRuntime(component, kind, programRef, "", "", props, capabilities, nil)
}

// AddEngineWithRuntime adds an engine entry with optional DOM mount, runtime
// selection, and pixel surface configuration.
func (m *Manifest) AddEngineWithRuntime(component, kind, programRef, mountID, runtime string, props any, capabilities []string, pixelSurface *engine.PixelSurfaceConfig) (string, error) {
	return m.AddEngineWithRuntimeRequirements(component, kind, programRef, mountID, runtime, props, capabilities, nil, pixelSurface)
}

// AddEngineWithRuntimeRequirements adds an engine entry with optional DOM
// mount, runtime selection, hard runtime capability requirements, and pixel
// surface configuration.
func (m *Manifest) AddEngineWithRuntimeRequirements(component, kind, programRef, mountID, runtime string, props any, capabilities, requiredCapabilities []string, pixelSurface *engine.PixelSurfaceConfig) (string, error) {
	component = strings.TrimSpace(component)
	kind = strings.TrimSpace(kind)
	programRef = strings.TrimSpace(programRef)
	mountID = strings.TrimSpace(mountID)
	runtime = strings.TrimSpace(runtime)
	if component == "" {
		return "", fmt.Errorf("engine component is required")
	}
	engineKind := engine.Kind(kind)
	if !engine.KindSupported(engineKind) {
		return "", fmt.Errorf("unsupported engine kind: %q", kind)
	}
	if err := engine.ValidateRuntime(engine.Runtime(runtime), programRef); err != nil {
		return "", err
	}
	propsJSON, err := json.Marshal(props)
	if err != nil {
		return "", err
	}
	id := engineID(len(m.Engines), mountID)
	entry := EngineEntry{
		ID:                   id,
		Component:            component,
		Kind:                 kind,
		ProgramRef:           programRef,
		MountID:              mountID,
		Runtime:              runtime,
		Props:                propsJSON,
		Capabilities:         capabilities,
		RequiredCapabilities: requiredCapabilities,
		PixelSurface:         pixelSurface,
	}
	m.Engines = append(m.Engines, entry)
	return id, nil
}

// AddSelfDescribingSurface records a self-describing DOM surface requirement.
// Compatible entries coalesce by kind, feature, runtime, and capabilities.
func (m *Manifest) AddSelfDescribingSurface(kind, feature, runtime string, count int, capabilities, requiredCapabilities []string) error {
	kind = strings.TrimSpace(kind)
	feature = strings.TrimSpace(feature)
	runtime = strings.TrimSpace(runtime)
	if kind == "" {
		return fmt.Errorf("self-describing surface kind is required")
	}
	var err error
	feature, err = NormalizeSelfDescribingSurfaceFeature(feature)
	if err != nil {
		return err
	}
	runtime, err = NormalizeSelfDescribingSurfaceRuntime(runtime)
	if err != nil {
		return err
	}
	if count <= 0 {
		count = 1
	}
	capabilities, err = normalizeSelfDescribingSurfaceCapabilities(capabilities)
	if err != nil {
		return err
	}
	requiredCapabilities, err = normalizeSelfDescribingSurfaceCapabilities(requiredCapabilities)
	if err != nil {
		return err
	}
	entry := SelfDescribingSurfaceEntry{
		Kind:                 kind,
		Feature:              feature,
		Runtime:              runtime,
		Count:                count,
		Capabilities:         append([]string(nil), capabilities...),
		RequiredCapabilities: append([]string(nil), requiredCapabilities...),
	}
	for i := range m.SelfDescribingSurfaces {
		if selfDescribingSurfaceCompatible(m.SelfDescribingSurfaces[i], entry) {
			m.SelfDescribingSurfaces[i].Count += count
			return nil
		}
	}
	m.SelfDescribingSurfaces = append(m.SelfDescribingSurfaces, entry)
	return nil
}

func selfDescribingSurfaceCompatible(a, b SelfDescribingSurfaceEntry) bool {
	return a.Kind == b.Kind &&
		normalizeSurfaceFeature(a.Feature) == normalizeSurfaceFeature(b.Feature) &&
		normalizeSurfaceRuntime(a.Runtime) == normalizeSurfaceRuntime(b.Runtime) &&
		stringSliceEqual(a.Capabilities, b.Capabilities) &&
		stringSliceEqual(a.RequiredCapabilities, b.RequiredCapabilities)
}

func normalizeSurfaceFeature(feature string) string {
	feature, _ = NormalizeSelfDescribingSurfaceFeature(feature)
	return feature
}

func normalizeSurfaceRuntime(runtime string) string {
	runtime, _ = NormalizeSelfDescribingSurfaceRuntime(runtime)
	return runtime
}

// NormalizeSelfDescribingSurfaceFeature applies the default feature and rejects
// values the browser bootstrap will ignore.
func NormalizeSelfDescribingSurfaceFeature(feature string) (string, error) {
	feature = strings.TrimSpace(feature)
	if feature == "" {
		feature = string(SelfDescribingSurfaceFeatureEngines)
	}
	switch SelfDescribingSurfaceFeature(feature) {
	case SelfDescribingSurfaceFeatureEngines,
		SelfDescribingSurfaceFeatureHubs,
		SelfDescribingSurfaceFeatureControllers,
		SelfDescribingSurfaceFeatureIslands,
		SelfDescribingSurfaceFeatureScene3D:
		return feature, nil
	default:
		return "", fmt.Errorf("unsupported self-describing surface feature: %q", feature)
	}
}

// NormalizeSelfDescribingSurfaceRuntime applies the default runtime and rejects
// values the browser bootstrap cannot honor for self-describing surfaces.
func NormalizeSelfDescribingSurfaceRuntime(runtime string) (string, error) {
	runtime = strings.TrimSpace(runtime)
	if runtime == "" {
		runtime = string(SelfDescribingSurfaceRuntimeShared)
	}
	switch SelfDescribingSurfaceRuntime(runtime) {
	case SelfDescribingSurfaceRuntimeShared:
		return runtime, nil
	default:
		return "", fmt.Errorf("unsupported self-describing surface runtime: %q", runtime)
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func normalizeSelfDescribingSurfaceCapabilities(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	caps := toEngineCapabilities(values)
	if err := engine.ValidateCapabilities(caps); err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, cap := range caps {
		value := strings.ToLower(strings.TrimSpace(string(cap)))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func toEngineCapabilities(values []string) []engine.Capability {
	out := make([]engine.Capability, 0, len(values))
	for _, value := range values {
		out = append(out, engine.Capability(value))
	}
	return out
}

// AddHub registers a realtime hub connection and returns the assigned ID.
func (m *Manifest) AddHub(name, path string, bindings []HubBinding) string {
	return m.AddHubWithInput(name, path, bindings, nil)
}

// AddHubWithInput registers a realtime hub connection with optional
// bootstrap-owned browser input forwarding.
func (m *Manifest) AddHubWithInput(name, path string, bindings []HubBinding, input *HubInputConfig) string {
	id := hubID(len(m.Hubs))
	m.Hubs = append(m.Hubs, HubEntry{
		ID:       id,
		Name:     name,
		Path:     path,
		Bindings: bindings,
		Input:    input,
	})
	return id
}

// AddController registers a declarative headless controller and returns the
// assigned stable manifest ID.
func (m *Manifest) AddController(config controller.Config) string {
	id := controllerID(len(m.Controllers))
	m.Controllers = append(m.Controllers, ControllerEntry{
		ID:     id,
		Config: config,
	})
	return id
}

// SetClientIdentity configures bootstrap-owned client identity state.
func (m *Manifest) SetClientIdentity(config ClientIdentityConfig) {
	m.ClientIdentity = &config
}

// SetTextureVariants copies the texture rows of a variant manifest onto the page
// manifest, so the client can select a variant without a second fetch.
//
// Only rows whose Kind is "texture" travel. An environment map, a model or an
// audio track needs a different consumer, and a texture selector that saw one
// could hand a texture binding a file that holds no pixels.
//
// The function refuses a variant with an empty URI. Everything else it copies
// verbatim, because assetpipe.BuildVariantManifest already dropped the variants
// the pipeline planned but never built.
func (m *Manifest) SetTextureVariants(manifest assetpipe.VariantManifest) {
	out := map[string][]ManifestVariantRef{}
	for _, asset := range manifest.Assets {
		path := strings.TrimSpace(asset.Path)
		if path == "" {
			continue
		}
		var refs []ManifestVariantRef
		for _, variant := range asset.Variants {
			if !strings.EqualFold(strings.TrimSpace(variant.Kind), "texture") {
				continue
			}
			uri := strings.TrimSpace(variant.URI)
			if uri == "" {
				continue
			}
			refs = append(refs, ManifestVariantRef{
				URI:                  uri,
				Quality:              variant.Quality,
				Bytes:                variant.Bytes,
				RequiredCapabilities: append([]string(nil), variant.RequiredCapabilities...),
			})
		}
		if len(refs) > 0 {
			out[path] = refs
		}
	}
	if len(out) == 0 {
		m.TextureVariants = nil
		return
	}
	m.TextureVariants = out
}

func controllerID(n int) string {
	return "gosx-controller-" + itoa(n)
}

// autoMountIDPrefix is island.Renderer.RenderEngine's own placeholder mount
// id ("gosx-engine-mount-<n>") for a mount-needing engine whose caller left
// Config.MountID empty. It carries no author identity, so it is still
// positional and engineID must not treat it as one — that pairing must stay
// in sync with island/island.go's fallback.
const autoMountIDPrefix = "gosx-engine-mount-"

// engineID assigns a stable engine id. An authored mount id derives the id
// directly (two engines never share a mount id, since it is also the DOM
// element the engine attaches to), so two engines on different routes with
// different mount ids never collide on a shared positional id. An engine
// with no mount id, or only the auto-generated placeholder mount id, falls
// back to the positional id.
func engineID(n int, mountID string) string {
	if mountID != "" && !strings.HasPrefix(mountID, autoMountIDPrefix) {
		return "gosx-engine-" + mountID
	}
	return "gosx-engine-" + itoa(n)
}

func hubID(n int) string {
	return "gosx-hub-" + itoa(n)
}

func computeIslandID(n int) string {
	return "gosx-compute-" + itoa(n)
}

func islandID(n int) string {
	return "gosx-island-" + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
