// Package controller defines GoSX's declarative headless browser controller
// contract. Controllers are page-scoped runtime records that bridge browser
// events, timers, fetch resources, storage, and shared signals without owning a
// DOM island or app-specific JavaScript.
package controller

// Config declares a page-scoped headless controller.
type Config struct {
	Name       string          `json:"name,omitempty"`
	Root       string          `json:"root,omitempty"`
	Inputs     []Input         `json:"inputs,omitempty"`
	Outputs    []Output        `json:"outputs,omitempty"`
	Events     []Event         `json:"events,omitempty"`
	Keys       []KeyBinding    `json:"keys,omitempty"`
	Drags      []DragBinding   `json:"drags,omitempty"`
	Focus      []FocusOwner    `json:"focus,omitempty"`
	Timers     []Timer         `json:"timers,omitempty"`
	Resources  []FetchResource `json:"resources,omitempty"`
	Storage    *Storage        `json:"storage,omitempty"`
	DisposeAll bool            `json:"disposeAll,omitempty"`
}

// NeedsInputRuntime reports whether the controller needs the optional input
// chunk. Ordinary events, keys, timers, and resources keep their
// existing runtime path.
func (c Config) NeedsInputRuntime() bool {
	if len(c.Drags)+len(c.Focus) > 0 || c.Storage != nil {
		return true
	}
	for _, event := range c.Events {
		if event.Project != nil {
			return true
		}
	}
	for _, key := range c.Keys {
		if key.Project != nil {
			return true
		}
	}
	return false
}

// Input mirrors a shared signal into the controller state. When Output is set,
// every received value is republished as a structured controller event.
type Input struct {
	Name      string `json:"name,omitempty"`
	Signal    string `json:"signal"`
	Output    string `json:"output,omitempty"`
	Immediate *bool  `json:"immediate,omitempty"`
}

// Output names a shared signal and/or a bubbling DOM CustomEvent. Signal
// receives the value; Event receives the same value as detail on the root.
// Event-only outputs let an app's action or hub adapter consume typed intents.
type Output struct {
	Name    string `json:"name,omitempty"`
	Signal  string `json:"signal,omitempty"`
	Event   string `json:"event,omitempty"`
	Initial any    `json:"initial,omitempty"`
}

// Event declares a delegated DOM event binding. Target is matched under Root
// when present; empty Target means the root itself.
type Event struct {
	Type            string      `json:"type"`
	Target          string      `json:"target,omitempty"`
	Output          string      `json:"output"`
	PreventDefault  bool        `json:"preventDefault,omitempty"`
	StopPropagation bool        `json:"stopPropagation,omitempty"`
	Capture         bool        `json:"capture,omitempty"`
	AllowEditable   bool        `json:"allowEditable,omitempty"`
	Project         *Projection `json:"project,omitempty"`
}

// KeyBinding declares a global or root-scoped keyboard binding.
type KeyBinding struct {
	Key       string   `json:"key,omitempty"`
	Code      string   `json:"code,omitempty"`
	Output    string   `json:"output"`
	Event     string   `json:"event,omitempty"`
	Scope     string   `json:"scope,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
	// AnyModifiers restores subset matching: extra held modifiers are allowed.
	// The default is exact, with unlisted Shift ignored for single symbol keys.
	AnyModifiers   bool        `json:"anyModifiers,omitempty"`
	PreventDefault bool        `json:"preventDefault,omitempty"`
	AllowEditable  bool        `json:"allowEditable,omitempty"`
	Project        *Projection `json:"project,omitempty"`
}

// Projection maps browser payloads and controller inputs into an application's
// typed intent shape. Value supplies constants/defaults (usually a Go struct).
// Fields maps top-level JSON field names to dot paths under event, inputs,
// or drag. Missing paths suppress the output; no expressions execute.
// Projected values are written directly, without the controller event envelope.
type Projection struct {
	Value  any               `json:"value,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
	// When requires scalar equality at each source path before emitting.
	When map[string]any `json:"when,omitempty"`
}

// DragBinding owns one pointer gesture from a delegated DOM source to a drop
// target. ThresholdPX defaults to 4; taps remain clicks. Output emits successful
// drops; the optional phase outputs emit start, move, and cancellation records.
// Source elements should use CSS touch-action: none for touch dragging.
type DragBinding struct {
	Source         string       `json:"source"`
	Targets        []DropTarget `json:"targets"`
	Output         string       `json:"output"`
	StartOutput    string       `json:"startOutput,omitempty"`
	MoveOutput     string       `json:"moveOutput,omitempty"`
	CancelOutput   string       `json:"cancelOutput,omitempty"`
	ThresholdPX    float64      `json:"thresholdPx,omitempty"`
	PreventDefault bool         `json:"preventDefault,omitempty"`
	Project        *Projection  `json:"project,omitempty"`
}

// DropTarget matches a DOM target under the controller root. Scene targets ask
// the Scene3D mount for a world ray and browser pick. To use native Go picking,
// set RequestOutput and ResultSignal together: an engine consumes PickRequest,
// raycasts its graph, then writes PickResult. HitIDs optionally restrict valid
// scene nodes. TimeoutMS defaults to 1000 for pending native results.
type DropTarget struct {
	Target        string   `json:"target"`
	Scene         bool     `json:"scene,omitempty"`
	HitIDs        []string `json:"hitIds,omitempty"`
	RequestOutput string   `json:"requestOutput,omitempty"`
	ResultSignal  string   `json:"resultSignal,omitempty"`
	TimeoutMS     int      `json:"timeoutMs,omitempty"`
}

// Vector3 is a world-space point or direction in a controller pick message.
// Its JSON representation matches scene.Vector3.
type Vector3 struct {
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
	Z float64 `json:"z,omitempty"`
}

// Ray is a world-space ray in a controller pick request.
// Its JSON representation matches scene.Ray.
type Ray struct {
	Origin    Vector3 `json:"origin"`
	Direction Vector3 `json:"direction"`
}

// RayHit describes a scene intersection in a controller pick result.
// Its JSON representation matches scene.RayHit.
type RayHit struct {
	ID            string  `json:"id,omitempty"`
	Kind          string  `json:"kind,omitempty"`
	Distance      float64 `json:"distance"`
	Point         Vector3 `json:"point"`
	Normal        Vector3 `json:"normal,omitzero"`
	Pickable      bool    `json:"pickable,omitempty"`
	InstanceIndex *int    `json:"instanceIndex,omitempty"`
	Method        string  `json:"method,omitempty"`
}

// PickRequest is emitted on a scene target's RequestOutput after pointer release.
// RequestID correlates the result with the live drag, including across remounts.
type PickRequest struct {
	RequestID string `json:"requestId"`
	Ray       Ray    `json:"ray"`
}

// PickResult carries a converted scene.RaycastGraph or SceneAccelerator.Raycast hit.
// A nil Hit is a miss. Results for expired, cancelled, or older drags are ignored.
type PickResult struct {
	RequestID string  `json:"requestId"`
	Hit       *RayHit `json:"hit"`
}

// FocusOwner owns a modal while OpenSignal is true. Target and optional focus
// selectors resolve under Root. It traps Tab/focus, makes background branches
// inert, and restores prior focus (or ReturnFocus) on close or disposal. Escape
// sets OpenSignal false. Nested owners give the most recently opened modal focus.
type FocusOwner struct {
	Target       string `json:"target"`
	OpenSignal   string `json:"openSignal"`
	InitialFocus string `json:"initialFocus,omitempty"`
	ReturnFocus  string `json:"returnFocus,omitempty"`
}

// Timer publishes a structured tick payload on Output.
type Timer struct {
	Name      string `json:"name,omitempty"`
	Output    string `json:"output"`
	EveryMS   int    `json:"everyMs"`
	Immediate bool   `json:"immediate,omitempty"`
	Payload   any    `json:"payload,omitempty"`
}

// FetchResource declares an abortable same-origin fetch resource. RefreshSignal
// triggers reloads, PollMS enables polling, and BodySignal can provide JSON
// request bodies for mutating resources.
type FetchResource struct {
	Name          string            `json:"name,omitempty"`
	URL           string            `json:"url"`
	Method        string            `json:"method,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Body          any               `json:"body,omitempty"`
	BodySignal    string            `json:"bodySignal,omitempty"`
	Output        string            `json:"output"`
	ErrorOutput   string            `json:"errorOutput,omitempty"`
	RefreshSignal string            `json:"refreshSignal,omitempty"`
	PollMS        int               `json:"pollMs,omitempty"`
	Immediate     *bool             `json:"immediate,omitempty"`
}

// Storage declares optional browser storage access. Loads can write decoded
// JSON directly to shared signals, publish structured controller events, or do
// both at controller start. Saves subscribe to signals and persist JSON values
// under the configured namespace.
type Storage struct {
	Area      string        `json:"area,omitempty"`
	Namespace string        `json:"namespace,omitempty"`
	Load      []StorageSlot `json:"load,omitempty"`
	Save      []StorageSlot `json:"save,omitempty"`
}

// StorageSlot maps a storage key to a shared Signal and/or controller Output.
// Signal receives a successfully decoded stored JSON value directly; missing,
// empty, or invalid storage leaves the signal's typed default unchanged.
// Output receives the structured storage event used by earlier controller
// configurations, including a nil value when no valid value was loaded.
type StorageSlot struct {
	Key    string `json:"key"`
	Signal string `json:"signal,omitempty"`
	Output string `json:"output,omitempty"`
}
