package budget

type Network struct {
	RTTMicros       int64 `json:"rttMicros"`
	DownBytesPerSec int64 `json:"downBytesPerSec"`
	UpBytesPerSec   int64 `json:"upBytesPerSec"`
}

type Networks struct {
	Slow4G Network `json:"slow4g"`
	P75    Network `json:"p75"`
}

// Profile fixes units, transport assumptions and the CPU reference class.
type Profile struct {
	Schema               string   `json:"schema"`
	ID                   string   `json:"id"`
	Version              int      `json:"version"`
	Reference            string   `json:"reference"`
	Width                int      `json:"width"`
	Height               int      `json:"height"`
	DPRMilli             int      `json:"dprMilli"`
	CPUMultiplierMilli   int      `json:"cpuMultiplierMilli"`
	BenchmarkIndexTarget *int64   `json:"benchmarkIndexTarget,omitempty"`
	Networks             Networks `json:"networks"`
	SetupRTTs            int      `json:"setupRtts"`
	FirstByteRTTs        int      `json:"firstByteRtts"`
	InitCwndBytes        int64    `json:"initCwndBytes"`
	QuantumBytes         int64    `json:"quantumBytes"`
}

func LoadProfile(path string, opts LoadOptions) (*Profile, error) {
	var p Profile
	if _, err := loadInput(path, opts, "Profile", &p); err != nil {
		return nil, err
	}
	if err := p.validate(); err != nil {
		return nil, inputReference(err, "profile", "/benchmarkIndexTarget")
	}
	return &p, nil
}

func (p Profile) validate() error {
	if p.Reference == "phone-4gb" && p.BenchmarkIndexTarget == nil {
		return invalidInput("/benchmarkIndexTarget")
	}
	return nil
}
