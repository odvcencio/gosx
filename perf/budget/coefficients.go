package budget

import (
	"errors"
	"time"
)

// Coefficient preserves unknown values and intervals as null, never zero.
type Coefficient struct {
	Name    string    `json:"name"`
	Value   *int64    `json:"value"`
	CI95    [2]*int64 `json:"ci95"`
	Status  string    `json:"status"`
	NVisits int       `json:"nVisits"`
	NBlocks int       `json:"nBlocks"`
	Method  string    `json:"method"`
}

type CoefficientSet struct {
	ID                 string        `json:"id"`
	Backend            string        `json:"backend"`
	Scenario           string        `json:"scenario"`
	Entries            []Coefficient `json:"entries"`
	PredictionErrorPPM *int64        `json:"predictionErrorPPM"`
}

type Coefficients struct {
	Schema        string           `json:"schema"`
	ProfileSHA256 string           `json:"profileSHA256"`
	Reference     string           `json:"reference"`
	MeasuredAt    string           `json:"measuredAt"`
	Sets          []CoefficientSet `json:"sets"`
}

func LoadCoefficients(path string, opts LoadOptions) (*Coefficients, error) {
	var c Coefficients
	if _, err := loadInput(path, opts, "Coefficients", &c); err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c Coefficients) validate() error {
	if _, err := time.Parse(time.RFC3339, c.MeasuredAt); err != nil {
		return errors.New("invalid coefficient timestamp")
	}
	ids := make(map[string]bool)
	for _, set := range c.Sets {
		if ids[set.ID] {
			return errors.New("duplicate coefficient set")
		}
		ids[set.ID] = true
		names := make(map[string]bool)
		for _, e := range set.Entries {
			if names[e.Name] {
				return errors.New("duplicate coefficient name")
			}
			names[e.Name] = true
			lo, hi := e.CI95[0], e.CI95[1]
			if (lo == nil) != (hi == nil) || lo != nil && (*lo > *hi || e.Value == nil || *e.Value < *lo || *e.Value > *hi) {
				return errors.New("invalid coefficient interval")
			}
			if e.NBlocks > e.NVisits {
				return errors.New("invalid coefficient support")
			}
			switch e.Status {
			case "unknown":
				if e.Value != nil || lo != nil || e.NVisits != 0 || e.NBlocks != 0 {
					return errors.New("unknown coefficient must have null value and no support")
				}
			case "unused":
				if e.Value == nil || *e.Value != 0 || e.Method != "unused" || e.NVisits != 0 || e.NBlocks != 0 {
					return errors.New("unused coefficient must be explicit zero")
				}
			case "provisional":
				if e.Value == nil || e.Method != "prior" {
					return errors.New("provisional coefficient requires a prior")
				}
			case "pilot", "measured":
				if e.Value == nil || lo == nil || e.NVisits == 0 || e.NBlocks == 0 || e.Method == "prior" || e.Method == "unused" {
					return errors.New("observed coefficient requires an interval and support")
				}
				// Spec 4.3 requires 30 visits and held-out error at most 200,000 ppm.
				if e.Status == "measured" && (e.NVisits < 30 || set.PredictionErrorPPM == nil || *set.PredictionErrorPPM > 200000) {
					return errors.New("measured coefficient requires accepted visits and held-out validation")
				}
			}
			if e.Name == "wasmOverlapPPM" && (e.Value != nil && *e.Value > 1000000 || hi != nil && *hi > 1000000) {
				return errors.New("overlap exceeds one million ppm")
			}
		}
	}
	return nil
}
