package cycle

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed data/public_validation.json
var publicReferenceJSON []byte

type PublicSource struct {
	ID, Title, URL, OriginalURL, Locator, Kind, Notes, PDFSHA256 string
}

type PublicRPMLimit struct {
	ID, Variant, Quantity, Source string
	MaximumRPM                    float64
}

type PublicExhaustLimit struct {
	ID, Variant, Source string
	MaximumC            float64
}

type PublicCandidate struct{ Title, URL, Reason string }

type publicReferenceData struct {
	ExhaustLimits []PublicExhaustLimit
	Protocol      string
	Sources       []PublicSource
	Limits        []PublicRPMLimit
	RPMReferences struct {
		Source, Notes                                                  string
		N1Gauge100RPM, N2Gauge100RPM, GroundIdleN2RPM, FlightIdleN2RPM float64
	}
	HotCase struct {
		Source, Variant, StationMapping                                  string
		AmbientTemperatureC, AmbientPressurePa, Mach, UninstalledThrustN float64
		CombustorOutletC, TurbineInletC                                  float64
	}
	NASAReferenceCases []struct {
		Mode                                                                       string
		FuelKGPerHour, ThrustN, CompressorOutletK, CompressorOutletPa, PrintedPSIA float64
	}
	NASAReferenceSource string
	ExcludedCandidates  []PublicCandidate
}

var publicReferenceOnce sync.Once
var publicReferences publicReferenceData

func publicData() *publicReferenceData {
	publicReferenceOnce.Do(func() {
		if err := json.Unmarshal(publicReferenceJSON, &publicReferences); err != nil {
			panic(err)
		}
	})
	return &publicReferences
}

// PublicComparison deliberately distinguishes an inequality, a nominal setting,
// and a reference-cycle comparison from a matched measured validation point.
// Difference is absent when operating conditions do not support comparison.
type PublicComparison struct {
	N1RPM, N2RPM                                                             float64 `json:",omitempty"`
	ID, Label, Category, Status, Quantity, Unit, Source, Conditions, Finding string
	Reference                                                                float64
	Prediction, Difference, RelativeDifference                               *float64      `json:",omitempty"`
	Command                                                                  *MappedInputs `json:",omitempty"`
	MatchingResidual                                                         *float64      `json:",omitempty"`
}

type PublicValidationReport struct {
	Profile, UID, Protocol, Conclusion, ReferenceDataSHA256, FrozenModelSHA256 string
	MatchedMeasuredPoints                                                      int
	Comparisons                                                                []PublicComparison
	Sources                                                                    []PublicSource
	ExcludedCandidates                                                         []PublicCandidate
}

func numberPointer(x float64) *float64 { return &x }

func compared(id, label, category, status, quantity, unit, source, conditions, finding string, ref, prediction float64, r MappedResult) PublicComparison {
	difference := prediction - ref
	return PublicComparison{ID: id, Label: label, Category: category, Status: status, Quantity: quantity, Unit: unit, Source: source, Conditions: conditions, Finding: finding, Reference: ref, Prediction: numberPointer(prediction), Difference: &difference, RelativeDifference: numberPointer(difference / ref), Command: &r.Command, MatchingResidual: &r.MatchResidual, N1RPM: r.N1RPM, N2RPM: r.N2RPM}
}

func publicLimitComparisons(r MappedResult) []PublicComparison {
	var rows []PublicComparison
	for _, limit := range publicData().Limits {
		prediction := r.N1RPM
		if limit.Quantity == "n2" {
			prediction = r.N2RPM
		}
		status, finding := "within-limit", "Within reported maximum; does not validate actual operating RPM."
		if prediction > limit.MaximumRPM {
			status, finding = "limit-exceeded", "Model RPM exceeds the reported maximum; current speed scaling is incompatible with this variant."
		}
		rows = append(rows, compared(limit.ID, limit.Variant+" "+limit.Quantity, "speed limit", status, limit.Quantity, "rpm", limit.Source,
			fmt.Sprintf("Model point: %.4f kg/s, %.2f K ambient, %.0f Pa, Mach %.3f", r.Command.FuelFlowKGS, r.Cycle.Ambient.TemperatureK, r.Cycle.Ambient.PressurePa, r.Command.Mach), finding, limit.MaximumRPM, prediction, r))
	}
	return rows
}

// recordPublicLimits only compares the current point; it never caps RPM or
// retunes a map. Clearing an invalid point also clears these live results.
func (d *EngineeringRecord) recordPublicLimits(r MappedResult) {
	d.Numbers["public/n1-gauge.1"] = r.N1RPM / publicData().RPMReferences.N1Gauge100RPM
	d.Numbers["public/n2-gauge.1"] = r.N2RPM / publicData().RPMReferences.N2Gauge100RPM
	d.Numbers["public/lpt-exhaust.K"] = r.Cycle.LowTurbine.TemperatureK
	d.Text["public/exhaust-status"] = "Within reported takeoff maximum; temperature unvalidated"
	for _, limit := range publicData().ExhaustLimits {
		if r.Cycle.LowTurbine.TemperatureK > limit.MaximumC+273.15 {
			d.Text["public/exhaust-status"] = "EXCEEDS reported takeoff exhaust-temperature maximum"
		}
	}
	exceeded := 0
	for _, q := range publicLimitComparisons(r) {
		prefix := "public/" + q.ID + "/"
		d.Numbers[prefix+"speed.rpm"] = *q.Prediction
		d.Numbers[prefix+"difference.rpm"] = *q.Difference
		d.Text[prefix+"status"] = "Within bound; RPM unvalidated"
		if q.Status == "limit-exceeded" {
			exceeded++
			d.Text[prefix+"status"] = "EXCEEDS published maximum"
		}
	}
	d.Text["public/status"] = "Within reported speed bounds; no matched measured RPM/station validation"
	if exceeded > 0 {
		d.Text["public/status"] = fmt.Sprintf("%d reported speed bounds exceeded; speed scaling needs correction", exceeded)
	}
}

// publicHotPoint conditions an additional point on published thrust and ambient
// temperature. Fuel is a solved input for this comparison, not a fitted model
// parameter. The small research bracket extends beyond the interactive fuel
// interval, while all five component-map bounds and seven equations remain in
// force. A missing bracket is reported, never extrapolated or forced to agree.
func (m *MappedModel) publicHotPoint() (MappedResult, error) {
	hot := publicData().HotCase
	if hot.AmbientPressurePa != PRef || hot.Mach != 0 {
		return MappedResult{}, fmt.Errorf("hot reference requires a supported static sea-level pressure")
	}
	command := MappedInputs{AmbientTemperatureK: hot.AmbientTemperatureC + 273.15}
	ratedFuel := reference.Points[0].FuelFlowKGS
	var previous MappedResult
	previousOK := false
	for i := 0; i <= 50; i++ {
		command.FuelFlowKGS = ratedFuel * (.90 + float64(i)*.005)
		r, err := m.solve(command)
		if err != nil {
			previousOK = false
			continue
		}
		if previousOK && (previous.Cycle.NetThrustN-hot.UninstalledThrustN)*(r.Cycle.NetThrustN-hot.UninstalledThrustN) <= 0 {
			lo, hi := previous.Command.FuelFlowKGS, command.FuelFlowKGS
			loError := previous.Cycle.NetThrustN - hot.UninstalledThrustN
			for range 34 {
				command.FuelFlowKGS = (lo + hi) / 2
				r, err = m.solve(command)
				if err != nil {
					return MappedResult{}, err
				}
				if (r.Cycle.NetThrustN-hot.UninstalledThrustN)*loError > 0 {
					lo = command.FuelFlowKGS
				} else {
					hi = command.FuelFlowKGS
				}
			}
			return r, nil
		}
		previous, previousOK = r, true
	}
	return MappedResult{}, fmt.Errorf("no hot-day thrust bracket inside the tested component-map region")
}

// ValidatePublicData freezes the existing calibration. A failed external check
// is evidence about the model, not an execution/test failure to hide by fitting.
func (m *MappedModel) ValidatePublicData() PublicValidationReport {
	data := publicData()
	modelBytes, _ := json.Marshal(m)
	report := PublicValidationReport{Profile: reference.Engine, UID: reference.UID, Protocol: data.Protocol,
		Conclusion:          "No matched measured RPM/station points with complete conditions were identified. Limits, nominal settings and reference specifications do not establish actual RPM/station accuracy.",
		ReferenceDataSHA256: fmt.Sprintf("%x", sha256.Sum256(publicReferenceJSON)), FrozenModelSHA256: fmt.Sprintf("%x", sha256.Sum256(modelBytes)),
		Sources: data.Sources, ExcludedCandidates: data.ExcludedCandidates}
	if rated, err := m.Solve(MappedInputs{FuelFlowKGS: reference.Points[0].FuelFlowKGS}); err == nil {
		limits := publicLimitComparisons(rated)
		report.Comparisons = append(report.Comparisons, limits...)
		for _, limit := range data.ExhaustLimits {
			status, finding := "within-limit", "Below the takeoff maximum, not independent temperature validation. Source Tt7 immediately downstream of the LPT maps to model total station 5; air-equivalent gas and sensor averaging remain approximations."
			if rated.Cycle.LowTurbine.TemperatureK > limit.MaximumC+273.15 {
				status, finding = "limit-exceeded", "Model total LPT-exit temperature exceeds the reported takeoff maximum."
			}
			report.Comparisons = append(report.Comparisons, compared(limit.ID, limit.Variant+" takeoff exhaust maximum", "exhaust temperature limit", status, "LPT exit total temperature", "K", limit.Source, "Takeoff (5 minutes), historical revision; source total Tt7 immediately downstream of low-pressure turbine discharge", finding, limit.MaximumC+273.15, rated.Cycle.LowTurbine.TemperatureK, rated))
		}
		for _, limit := range limits {
			if limit.Status == "limit-exceeded" {
				report.Conclusion += " Rated model RPM exceeds a published standard-variant maximum."
				break
			}
		}
	}
	if idle, err := m.Solve(MappedInputs{FuelFlowKGS: .2054}); err == nil {
		report.Comparisons = append(report.Comparisons, PublicComparison{ID: "nominal-ground-idle-n2", Label: "Nominal ground-idle N2", Category: "nominal reference", Status: "conditions-missing", Quantity: "n2", Unit: "rpm", Source: data.RPMReferences.Source, Reference: data.RPMReferences.GroundIdleN2RPM, Prediction: numberPointer(idle.N2RPM), Command: &idle.Command,
			Conditions: "Reference ambient, fuel, bleed and load are unspecified; model is the ICAO idle fuel at 15 C.", Finding: "Visible discrepancy; no matched-condition RPM error or pass/fail can be assigned."})
	}
	report.Comparisons = append(report.Comparisons, PublicComparison{ID: "nominal-flight-idle-n2", Label: "Nominal flight-idle N2", Category: "nominal reference", Status: "conditions-missing", Quantity: "n2", Unit: "rpm", Source: data.RPMReferences.Source, Reference: data.RPMReferences.FlightIdleN2RPM, Conditions: "No flight condition, fuel or control schedule is supplied.", Finding: "Not compared to the ground-idle model."})
	hot := data.HotCase
	warm, warmErr := m.publicHotPoint()
	for _, q := range []struct {
		id, label, station string
		ref, prediction    float64
	}{
		{"hot-combustor-outlet", "Hot-day combustor outlet", "4", hot.CombustorOutletC + 273.15, warm.Cycle.Burner.TemperatureK},
		{"hot-turbine-inlet", "Hot-day cooled turbine inlet", "41", hot.TurbineInletC + 273.15, warm.TurbineInlet.TemperatureK},
	} {
		conditions := fmt.Sprintf("%s; %.1f C, 101325 Pa, static; %.1f kN uninstalled thrust used as an input; station %s", hot.Variant, hot.AmbientTemperatureC, hot.UninstalledThrustN/1000, q.station)
		if warmErr != nil {
			report.Comparisons = append(report.Comparisons, PublicComparison{ID: q.id, Label: q.label, Category: "published specification", Status: "unavailable", Quantity: "total temperature", Unit: "K", Source: hot.Source, Reference: q.ref, Conditions: conditions, Finding: warmErr.Error()})
			continue
		}
		status := "qualified-comparison"
		finding := hot.StationMapping + " Exhibit specifications lack instrument/uncertainty metadata. Temperature differences are descriptive, not acceptance tolerances; the hot point is outside the UI fuel interval."
		for _, limit := range publicLimitComparisons(warm) {
			if limit.Status == "limit-exceeded" {
				status = "speed-incompatible"
				finding += fmt.Sprintf(" The matched hot point violates reported speed bounds (model N1 %.1f, N2 %.1f rpm); it is not a physically validated engine condition.", warm.N1RPM, warm.N2RPM)
				break
			}
		}
		report.Comparisons = append(report.Comparisons, compared(q.id, q.label, "published specification", status, "total temperature", "K", hot.Source, conditions, finding, q.ref, q.prediction, warm))
	}
	for _, p := range data.NASAReferenceCases {
		in := MappedInputs{FuelFlowKGS: p.FuelKGPerHour / 3600}
		r, err := m.Solve(in)
		for _, q := range []struct {
			suffix, quantity, unit string
			ref, prediction        float64
		}{
			{"thrust", "net thrust", "N", p.ThrustN, r.Cycle.NetThrustN},
			{"temperature", "compressor outlet temperature", "K", p.CompressorOutletK, r.Cycle.Compressor.TemperatureK},
			{"pressure", "compressor outlet pressure", "Pa", p.CompressorOutletPa, r.Cycle.Compressor.PressurePa},
		} {
			conditions := fmt.Sprintf("JT9D-7A %s reference versus -7R4D/D1 model; standard day, static; %.0f kg/h fuel / 3600", p.Mode, p.FuelKGPerHour)
			finding := "Earlier variant and reference cycle: contextual comparison, not validation of -7R4D/D1. Source combustor inlet Tt4 maps to model station 3."
			if p.Mode == "takeoff" && q.unit == "Pa" {
				finding += " Source SI/psia columns disagree: use printed 21.68e5 Pa, not 323 psia; do not silently reconcile."
			}
			if err != nil {
				report.Comparisons = append(report.Comparisons, PublicComparison{ID: "nasa-" + p.Mode + "-" + q.suffix, Label: "NASA -7A " + p.Mode + " " + q.quantity, Category: "earlier-variant reference", Status: "unavailable", Quantity: q.quantity, Unit: q.unit, Source: data.NASAReferenceSource, Reference: q.ref, Conditions: conditions, Finding: err.Error()})
				continue
			}
			report.Comparisons = append(report.Comparisons, compared("nasa-"+p.Mode+"-"+q.suffix, "NASA -7A "+p.Mode+" "+q.quantity, "earlier-variant reference", "variant-mismatch", q.quantity, q.unit, data.NASAReferenceSource, conditions, finding, q.ref, q.prediction, r))
		}
	}
	return report
}
