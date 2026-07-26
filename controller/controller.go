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
	Timers     []Timer         `json:"timers,omitempty"`
	Resources  []FetchResource `json:"resources,omitempty"`
	Storage    *Storage        `json:"storage,omitempty"`
	DisposeAll bool            `json:"disposeAll,omitempty"`
}

// Input mirrors a shared signal into the controller state. When Output is set,
// every received value is republished as a structured controller event.
type Input struct {
	Name      string `json:"name,omitempty"`
	Signal    string `json:"signal"`
	Output    string `json:"output,omitempty"`
	Immediate *bool  `json:"immediate,omitempty"`
}

// Output describes a shared signal the controller writes.
type Output struct {
	Name    string `json:"name,omitempty"`
	Signal  string `json:"signal"`
	Initial any    `json:"initial,omitempty"`
}

// Event declares a delegated DOM event binding. Target is matched under Root
// when present; empty Target means the root itself.
type Event struct {
	Type            string `json:"type"`
	Target          string `json:"target,omitempty"`
	Output          string `json:"output"`
	PreventDefault  bool   `json:"preventDefault,omitempty"`
	StopPropagation bool   `json:"stopPropagation,omitempty"`
	Capture         bool   `json:"capture,omitempty"`
	AllowEditable   bool   `json:"allowEditable,omitempty"`
}

// KeyBinding declares a global or root-scoped keyboard binding.
type KeyBinding struct {
	Key            string   `json:"key,omitempty"`
	Code           string   `json:"code,omitempty"`
	Output         string   `json:"output"`
	Event          string   `json:"event,omitempty"`
	Scope          string   `json:"scope,omitempty"`
	Modifiers      []string `json:"modifiers,omitempty"`
	PreventDefault bool     `json:"preventDefault,omitempty"`
	AllowEditable  bool     `json:"allowEditable,omitempty"`
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

// Storage declares optional browser storage access. Loads publish item values
// to outputs at controller start; Saves subscribe to input signals and persist
// JSON values under the configured namespace.
type Storage struct {
	Area      string        `json:"area,omitempty"`
	Namespace string        `json:"namespace,omitempty"`
	Load      []StorageSlot `json:"load,omitempty"`
	Save      []StorageSlot `json:"save,omitempty"`
}

// StorageSlot maps storage keys to shared signals.
type StorageSlot struct {
	Key    string `json:"key"`
	Signal string `json:"signal,omitempty"`
	Output string `json:"output,omitempty"`
}
