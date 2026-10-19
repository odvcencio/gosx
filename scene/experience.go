package scene

// Experience groups runtime controls authored in Go. A Scene3D ExperienceRef
// can load this same-origin descriptor once before mounting, keeping interactive
// definitions out of the initial document and static geometry payload.
type Experience struct {
	Optics       *Optics
	Presentation *Presentation
	DayCycle     *DayCycle
	Exploration  *Exploration
	Vessel       *Vessel
}

func (e Experience) GoSXProps() map[string]any {
	out := map[string]any{}
	if e.Optics != nil {
		out["optics"] = e.Optics
	}
	if e.Presentation != nil {
		out["presentation"] = e.Presentation
	}
	if e.DayCycle != nil {
		out["dayCycle"] = e.DayCycle
	}
	if e.Exploration != nil {
		out["exploration"] = e.Exploration.GoSXProps()
	}
	if e.Vessel != nil {
		out["vessel"] = e.Vessel
	}
	return out
}
