package scene

// Presentation changes viewpoints in one mounted world. The engine preserves
// simulations and collected objects when a GoSX island selects a view.
type Presentation struct {
	Endpoint      string               `json:"endpoint,omitempty"` // optional same-origin command loader
	InputSignal   string               `json:"inputSignal"`
	OutputSignal  string               `json:"outputSignal"`
	InitialView   string               `json:"initialView"`
	InitialPeriod string               `json:"initialPeriod"`
	Views         []PresentationView   `json:"views"`
	Periods       []PresentationPeriod `json:"periods,omitempty"`
	Query         bool                 `json:"query,omitempty"` // reflect view/period in this page's URL
}

type PresentationView struct {
	ID     string   `json:"id"`
	Label  string   `json:"label"`
	Camera IRCamera `json:"camera"`           // with TransitionMS above zero the page eases to it, as for SetCamera
	Vessel bool     `json:"vessel,omitempty"` // return to a sailing helm without teleporting
}

type PresentationPeriod struct {
	ID       string    `json:"id"`
	Commands []Command `json:"commands"`
	Hour     float64   `json:"hour"` // optional DayCycle position; cycle continues from here
}

// Revision distinguishes repeated input requests, including resetting the
// current viewpoint. Zero values leave the other presentation selection alone.
type PresentationRequest struct {
	View, Period string
	Reset        bool
	Revision     int
}
type PresentationState struct {
	Ready        bool
	View, Period string
}
