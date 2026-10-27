package cycle

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestMappedAmbientConditionPreservesPressureAndBalances(t *testing.T) {
	m, err := JT9D()
	if err != nil {
		t.Fatal(err)
	}
	isa, err := m.Solve(MappedInputs{FuelFlowKGS: 1.678})
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := m.Solve(MappedInputs{FuelFlowKGS: 1.678, AmbientTemperatureK: TRef})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(isa.Cycle.NetThrustN-explicit.Cycle.NetThrustN) > 1e-7 || math.Abs(isa.N2RPM-explicit.N2RPM) > 1e-7 {
		t.Fatal("explicit ISA temperature changed the existing point")
	}
	warm, err := m.Solve(MappedInputs{FuelFlowKGS: 1.678, AmbientTemperatureK: 306.45, Mach: .2})
	if err != nil {
		t.Fatal(err)
	}
	r := warm.Cycle
	if r.Ambient.TemperatureK != 306.45 || r.Ambient.PressurePa != PRef {
		t.Fatalf("temperature override changed static pressure: %+v", r.Ambient)
	}
	if math.Abs(gasH(r.Inlet.TemperatureK)-gasH(r.Ambient.TemperatureK)-r.FlightSpeedMPS*r.FlightSpeedMPS/2) > 1e-6 {
		t.Fatal("warm inlet total enthalpy does not include flight kinetic energy")
	}
	if math.Abs(gasS(r.Inlet.TemperatureK)-gasS(r.Ambient.TemperatureK)-R*math.Log(r.Inlet.PressurePa/r.Ambient.PressurePa)) > 1e-7 {
		t.Fatal("warm inlet total pressure is not isentropic")
	}
	if warm.MatchResidual >= 1e-9 || r.MaxRelativeResidual >= 1e-7 {
		t.Fatal("warm point does not conserve/match")
	}
	for _, temperature := range []float64{math.NaN(), -1, 199, 331} {
		if _, err := m.Solve(MappedInputs{FuelFlowKGS: 1.678, AmbientTemperatureK: temperature}); err == nil {
			t.Fatalf("accepted invalid ambient temperature %g", temperature)
		}
	}
	if _, err := m.Solve(MappedInputs{FuelFlowKGS: 2.13, AmbientTemperatureK: 306.45}); err == nil {
		t.Fatal("public interactive API silently widened its calibrated fuel interval")
	}
}

func TestPublicReferenceAuditDoesNotRefitOrOverstateEvidence(t *testing.T) {
	m, err := JT9D()
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(m)
	report := m.ValidatePublicData()
	after, _ := json.Marshal(m)
	if !bytes.Equal(before, after) {
		t.Fatal("external reference comparison changed model parameters")
	}
	if report.MatchedMeasuredPoints != 0 || len(report.Comparisons) != 22 || len(report.Sources) != 3 {
		t.Fatal("reference categories/counts misstate the evidence")
	}
	rows := map[string]PublicComparison{}
	for _, row := range report.Comparisons {
		rows[row.ID] = row
		if row.Prediction != nil {
			t.Logf("%s: reference %.6f, model %.6f %s, status %s", row.ID, row.Reference, *row.Prediction, row.Unit, row.Status)
		}
	}
	for _, id := range []string{"hot-combustor-outlet", "hot-turbine-inlet"} {
		q := rows[id]
		if (q.Status != "qualified-comparison" && q.Status != "speed-incompatible") || q.Command == nil || q.Prediction == nil || q.MatchingResidual == nil || *q.MatchingResidual >= 1e-9 {
			t.Fatalf("hot-day comparison unavailable or misclassified: %+v", q)
		}
		if math.Abs(q.Command.AmbientTemperatureK-306.45) > 1e-10 || q.Command.FuelFlowKGS <= 2.055 {
			t.Fatal("hot comparison did not condition the separate warm point")
		}
		r, err := m.solve(*q.Command)
		if err != nil || math.Abs(r.Cycle.NetThrustN-213500) > 1e-4 || r.Cycle.MaxRelativeResidual >= 1e-7 {
			t.Fatalf("hot point did not reproduce reported thrust/closure: %v", err)
		}
		if math.Abs(*q.RelativeDifference-(*q.Prediction/q.Reference-1)) > 1e-12 {
			t.Fatal("reported temperature difference is incorrect")
		}
	}
	if math.Abs(rows["hot-combustor-outlet"].Reference-1544.25) > 1e-9 || math.Abs(rows["hot-turbine-inlet"].Reference-1458.15) > 1e-9 {
		t.Fatal("source Celsius-to-Kelvin conversion is incorrect")
	}
	for _, id := range []string{"nominal-ground-idle-n2", "nominal-flight-idle-n2"} {
		q := rows[id]
		if q.Status != "conditions-missing" || q.Difference != nil || q.RelativeDifference != nil {
			t.Fatal("unmatched idle reference was assigned a validation error")
		}
	}
	if rows["nominal-flight-idle-n2"].Prediction != nil {
		t.Fatal("ground model was used as a flight-idle observation")
	}
	if rows["nasa-takeoff-pressure"].Reference != 2168000 || !strings.Contains(rows["nasa-takeoff-pressure"].Finding, "disagree") {
		t.Fatal("source SI/psia discrepancy was concealed")
	}
	for id, row := range rows {
		if strings.HasPrefix(id, "nasa-") && row.Status != "variant-mismatch" {
			t.Fatal("earlier-variant reference was counted as matched engine validation")
		}
	}
}

func TestPublicSpeedLimitsStayLiveAndExposeFailures(t *testing.T) {
	m, err := JT9D()
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.Solve(MappedInputs{FuelFlowKGS: 2.055})
	if err != nil {
		t.Fatal(err)
	}
	// A deliberately oversized speed must stay visible as a failed inequality;
	// reference checks must not clamp the solution or label it validated.
	r.N1RPM, r.N2RPM = 4000, 8100
	r.Cycle.LowTurbine.TemperatureK = 900
	d := Analyze(r.Cycle, &r, m, nil)
	for _, id := range []string{"d-n1", "d1-n1", "d-n2", "d1-n2"} {
		if !strings.Contains(d.Text["public/"+id+"/status"], "EXCEEDS") {
			t.Fatalf("speed discrepancy hidden: %s", id)
		}
	}
	if math.Abs(d.Numbers["public/d-n1/difference.rpm"]-230) > 1e-12 || math.Abs(d.Numbers["public/n1-gauge.1"]-4000.0/3600) > 1e-12 {
		t.Fatal("limit difference or gauge normalization is incorrect")
	}
	if !strings.Contains(d.Text["public/exhaust-status"], "EXCEEDS") {
		t.Fatal("exhaust-temperature exceedance hidden")
	}
	if r.N1RPM != 4000 || r.N2RPM != 8100 {
		t.Fatal("checking references changed the operating point")
	}
	if !strings.Contains(d.CSV(), "public/d-n1/speed,4000,rpm") {
		t.Fatal("engineering export lost the failed speed check or its units")
	}
	bad := Analyze(r.Cycle, &r, m, errors.New("infeasible"))
	if _, ok := bad.Numbers["public/d-n1/speed.rpm"]; ok {
		t.Fatal("invalid point kept a stale public RPM comparison")
	}
	generic, err := DefaultModel().Solve(DefaultInputs())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Analyze(generic, nil, m, nil).Numbers["public/d-n1/speed.rpm"]; ok {
		t.Fatal("generic design point acquired a JT9D speed validation")
	}
}
