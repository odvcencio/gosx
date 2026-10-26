package scene

// Exploration links scene picking to a persistent, local field journal.
// Proximity is local interaction gating, not server authorization.
type Exploration struct {
	RevealSignal   string   `json:"revealSignal,omitempty"`
	DismissSignals []string `json:"dismissSignals,omitempty"`

	InputSignal  string       `json:"inputSignal,omitempty"`
	OutputSignal string       `json:"outputSignal"`
	StorageKey   string       `json:"storageKey,omitempty"`
	Items        []Inspection `json:"items"`
	Pocket       *Pocket      `json:"pocket,omitempty"`
}

type Inspection struct {
	ID       string           `json:"id"`
	Label    string           `json:"label"`
	Position *Vector3         `json:"position,omitempty"`
	Radius   float64          `json:"radius,omitempty"`
	Collect  bool             `json:"collect,omitempty"`
	Commands []Command        `json:"commands,omitempty"`
	Pulse    *InspectionPulse `json:"pulse,omitempty"`
	Sound    *SynthPatch      `json:"-"`
}

type InspectionPulse struct {
	NodeID          string  `json:"nodeId"`
	Position        Vector3 `json:"position"`
	Radius          float64 `json:"radius"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
}

// Pocket keeps a same-origin embedded engine alive while its dialog is put
// away. The child saves and pauses on pause, and resumes the same instance.
type Pocket struct {
	PauseBackground bool `json:"pauseBackground,omitempty"` // preserve held-course sailing while pausing other background scenes

	DialogID   string `json:"dialogId"`
	Source     string `json:"source"`
	Title      string `json:"title"`
	Protocol   string `json:"protocol"`
	OpenSignal string `json:"openSignal"`
}

type InspectionRequest struct {
	ID       string
	Revision int
}
type JournalState struct {
	Found                        map[string]bool
	CountLabel, ActiveID, Notice string
	Pocket                       bool
}

func (e Exploration) GoSXProps() map[string]any {
	items := make([]map[string]any, 0, len(e.Items))
	for _, item := range e.Items {
		p := map[string]any{"id": item.ID, "label": item.Label, "radius": item.Radius, "collect": item.Collect, "commands": item.Commands, "position": item.Position, "pulse": item.Pulse}
		if item.Sound != nil {
			p["sound"] = item.Sound.Props()
		}
		items = append(items, p)
	}
	return map[string]any{"revealSignal": e.RevealSignal, "dismissSignals": e.DismissSignals, "inputSignal": e.InputSignal, "outputSignal": e.OutputSignal, "storageKey": e.StorageKey, "items": items, "pocket": e.Pocket}
}
