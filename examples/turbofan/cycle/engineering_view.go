package cycle

import (
	"fmt"
	"strconv"
)

type DiagnosticCell struct {
	Key, Format, Value string
	Scale              float64
	Digits             int
}
type DiagnosticRow struct {
	Label, Class string
	Cells        []DiagnosticCell
}
type DiagnosticTable struct {
	ID, Title, Note, Class string
	Headers                []string
	Rows                   []DiagnosticRow
}

// Table labels and column definitions run only on the server. The browser
// updates SI quantities using the field/scale/format bindings in the HTML.
func DiagnosticTables(d EngineeringRecord) []DiagnosticTable {
	cell := func(key string, scale float64, digits int) DiagnosticCell {
		c := DiagnosticCell{Key: key, Scale: scale, Digits: digits, Format: "f", Value: "—"}
		if x, ok := d.Numbers[key]; ok {
			c.Value = strconv.FormatFloat(x*scale, 'f', digits, 64)
		}
		return c
	}
	sci := func(key string) DiagnosticCell {
		c := cell(key, 1, 2)
		c.Format = "e"
		if x, ok := d.Numbers[key]; ok {
			c.Value = strconv.FormatFloat(x, 'e', 2, 64)
		}
		return c
	}
	text := func(key string) DiagnosticCell {
		value := d.Text[key]
		if value == "" {
			value = "—"
		}
		return DiagnosticCell{Key: key, Format: "t", Value: value}
	}
	power := DiagnosticTable{ID: "power-ledger", Title: "Fuel, airflow & shaft work", Headers: []string{"Quantity", "Model value"}, Note: "HP/LP shaft work is internal transfer. Whole-engine closure uses inlet/exhaust total enthalpy, heat delivered to gas and shaft loss. Kinetic power is part of exhaust total enthalpy; it is not a thermal-efficiency claim. F·V is zero in a static test. Torque uses estimated RPM."}
	for _, q := range []struct {
		label, key string
		scale      float64
		digits     int
	}{
		{"Total inlet air · kg/s", "air/total.kg_s", 1, 3}, {"Core air · kg/s", "air/core.kg_s", 1, 3}, {"Bypass air · kg/s", "air/bypass.kg_s", 1, 3},
		{"Fuel/combustor-air ratio · kg/kg", "fuel/air-ratio.1", 1, 6}, {"HPT inlet cooling · kg/s", "fuel/hpt-inlet-cooling.kg_s", 1, 3}, {"HPT exit cooling · kg/s", "fuel/hpt-exit-cooling.kg_s", 1, 3},
		{"Fuel chemical power · MW", "fuel/chemical-power.W", 1e-6, 3}, {"Heat delivered to gas · MW", "fuel/heat-to-gas.W", 1e-6, 3}, {"Combustion loss · MW", "fuel/burner-loss.W", 1e-6, 3},
		{"LP shaft load · MW", "power/lp-load.W", 1e-6, 3}, {"LP turbine delivered · MW", "power/lp-delivered.W", 1e-6, 3}, {"HP shaft load · MW", "power/hp-load.W", 1e-6, 3}, {"HP turbine delivered · MW", "power/hp-delivered.W", 1e-6, 3},
		{"Mechanical shaft loss · MW", "power/shaft-loss.W", 1e-6, 3}, {"LP load torque · kN·m", "power/lp-torque.Nm", 1e-3, 3}, {"HP load torque · kN·m", "power/hp-torque.Nm", 1e-3, 3},
		{"Exhaust kinetic power · MW", "power/exhaust-kinetic.W", 1e-6, 3}, {"Inlet kinetic power · MW", "power/inlet-kinetic.W", 1e-6, 3}, {"Thrust power F·V · MW", "power/thrust.W", 1e-6, 3},
		{"Inlet total enthalpy flux · MW", "power/inlet-total-enthalpy.W", 1e-6, 3}, {"Exhaust total enthalpy flux · MW", "power/exhaust-total-enthalpy.W", 1e-6, 3}, {"Exhaust static enthalpy flux · MW", "power/exhaust-static-enthalpy.W", 1e-6, 3},
		{"Specific thrust · N·s/kg inlet air", "thrust/specific.Ns_kg", 1, 3},
	} {
		power.Rows = append(power.Rows, DiagnosticRow{Label: q.label, Cells: []DiagnosticCell{cell(q.key, q.scale, q.digits)}})
	}
	thrust := DiagnosticTable{ID: "thrust-ledger", Title: "Where the thrust comes from", Headers: []string{"Stream", "Exit momentum · kN", "Ram drag · kN", "Pressure thrust · kN", "Net · kN", "Share · %"}, Note: "F = ṁexit Vexit − ṁair Vflight + (pexit − pamb) A. Core exit mass includes added fuel. The stream totals reproduce net thrust."}
	nozzles := DiagnosticTable{ID: "nozzle-diagnostics", Title: "Nozzle discharge & choking", Headers: []string{"Stream", "Pt / pamb", "Critical NPR", "NPR / critical", "Exit M", "Velocity · m/s", "Sound · m/s", "Density · kg/m³", "Mass flux · kg/(m²·s)", "State"}, Note: "Critical NPR is the sonic pressure ratio from the selected gas model. A ratio above 1 in the NPR/critical column means choking. Density and sound speed use static exit conditions."}
	for _, name := range []string{"core", "bypass"} {
		p := "nozzle/" + name + "/"
		thrust.Rows = append(thrust.Rows, DiagnosticRow{Label: name, Cells: []DiagnosticCell{cell(p+"momentum-thrust.N", 1e-3, 3), cell(p+"ram-drag.N", 1e-3, 3), cell(p+"pressure-thrust.N", 1e-3, 3), cell(p+"net-thrust.N", 1e-3, 3), cell(p+"thrust-share.1", 100, 2)}})
		nozzles.Rows = append(nozzles.Rows, DiagnosticRow{Label: name, Cells: []DiagnosticCell{cell(p+"npr.1", 1, 4), cell(p+"critical-npr.1", 1, 4), cell(p+"choking-ratio.1", 1, 4), cell(p+"mach.1", 1, 4), cell(p+"velocity.m_s", 1, 2), cell(p+"sound-speed.m_s", 1, 2), cell(p+"density.kg_m3", 1, 4), cell(p+"mass-flux.kg_m2s", 1, 2), text(p + "state")}})
	}
	components := DiagnosticTable{ID: "component-diagnostics", Title: "Component loading & efficiency", Headers: []string{"Component", "Tin · K", "Tout · K", "Pressure ratio", "Model η · %", "Gas Δḣ · MW", "Corrected flow · kg/s"}, Note: "Positive gas power is compression work; negative gas power is turbine extraction before shaft loss. Fan power covers both air streams. Booster power covers core air. Model efficiencies include fitted corrections; they are not measured component efficiencies."}
	maps := DiagnosticTable{ID: "map-coordinates", Title: "Corrected speeds & map coverage", Class: "mapped-row", Headers: []string{"Component", "Corrected speed · rpm", "Map Nc", "Map coordinate", "Published Nc range", "Published coordinate range", "Current region"}, Note: "Nc = N / √(Tin/288.15). Map Nc uses NASA's native map units; turbine and compressor axes use different scales. Coordinate means compressor R-line or turbine pressure-axis coordinate. Table coverage is not surge margin."}
	for _, name := range []string{"Fan", "LPC", "HPC", "HPT", "LPT"} {
		p := "component/" + name + "/"
		class := ""
		if name == "LPC" {
			class = "mapped-row"
		}
		components.Rows = append(components.Rows, DiagnosticRow{Label: name, Class: class, Cells: []DiagnosticCell{cell(p+"tin.K", 1, 2), cell(p+"tout.K", 1, 2), cell(p+"pressure-ratio.1", 1, 4), cell(p+"efficiency.1", 100, 2), cell(p+"gas-power.W", 1e-6, 3), cell(p+"corrected-flow.kg_s", 1, 3)}})
		raw := rawMaps[name]
		nRange := DiagnosticCell{Value: fmt.Sprintf("%g–%g", raw.Speed[0], raw.Speed[len(raw.Speed)-1])}
		cRange := DiagnosticCell{Value: fmt.Sprintf("%g–%g", raw.Coordinate[0], raw.Coordinate[len(raw.Coordinate)-1])}
		maps.Rows = append(maps.Rows, DiagnosticRow{Label: name, Cells: []DiagnosticCell{cell(p+"corrected-speed.rpm", 1, 1), cell(p+"map-speed.1", 1, 4), cell(p+"map-coordinate.1", 1, 4), nRange, cRange, text(p + "region")}})
	}
	stations := DiagnosticTable{ID: "station-thermodynamics", Title: "Full station thermodynamics", Headers: []string{"Station", "T · K", "P · kPa", "Flow · kg/s", "h · kJ/kg", "s · kJ/(kg·K)", "cp · kJ/(kg·K)", "γ", "Corrected flow · kg/s"}, Note: "Total states except static nozzle exits 9 and 19. h and s are relative to 288.15 K and 101.325 kPa. Corrected flow uses total states, so it is omitted at nozzle exits. The gas is air-equivalent, including the hot stream. Station 45a is a model tag before the second cooling mix."}
	for _, q := range []struct{ id, label string }{{"2", "2 · Inlet"}, {"13", "13 · Fan exit"}, {"25", "25 · Booster exit"}, {"3", "3 · HPC exit"}, {"4", "4 · Burner exit"}, {"41", "41 · Cooled HPT inlet"}, {"45a", "45a · HPT before mix"}, {"45", "45 · LPT inlet / mixed"}, {"5", "5 · LPT exit"}, {"9", "9 · Core exit (static)"}, {"19", "19 · Bypass exit (static)"}} {
		p, class := "station/"+q.id+"/", ""
		if q.id == "25" || q.id == "41" || q.id == "45a" {
			class = "mapped-row"
		}
		stations.Rows = append(stations.Rows, DiagnosticRow{Label: q.label, Class: class, Cells: []DiagnosticCell{cell(p+"temperature.K", 1, 2), cell(p+"pressure.Pa", 1e-3, 3), cell(p+"flow.kg_s", 1, 4), cell(p+"enthalpy.J_kg", 1e-3, 3), cell(p+"entropy.J_kgK", 1e-3, 5), cell(p+"cp.J_kgK", 1e-3, 5), cell(p+"gamma.1", 1, 5), cell(p+"corrected-flow.kg_s", 1, 4)}})
	}
	closure := DiagnosticTable{ID: "conservation-audit", Title: "Mass & energy closure", Headers: []string{"Balance", "Signed residual"}, Note: "Residuals measure numerical closure of the model equations. Small residuals do not establish agreement with an engine measurement. Signed values preserve the direction of the error."}
	for _, q := range []struct{ label, key string }{{"Burner · W", "closure/burner.W"}, {"HP shaft · W", "closure/hp.W"}, {"LP shaft · W", "closure/lp.W"}, {"Whole engine · W", "closure/energy.W"}, {"Mass · kg/s", "closure/mass.kg_s"}, {"Maximum relative balance error", "closure/relative.1"}} {
		closure.Rows = append(closure.Rows, DiagnosticRow{Label: q.label, Cells: []DiagnosticCell{sci(q.key)}})
	}
	residuals := DiagnosticTable{ID: "matching-audit", Title: "Seven-equation matching audit", Class: "mapped-row", Headers: []string{"Constraint", "Signed SI residual", "Normalized residual"}, Note: "Normalized residuals use rated reference flows or shaft powers. Convergence requires max |normalized residual| < 1e−9. Iteration count includes fuel-continuation solves, rather than one Newton step sequence."}
	for i, label := range []string{"HPC flow · kg/s", "HPT flow · kg/s", "HP shaft · W", "LPT flow · kg/s", "LP shaft · W", "Core nozzle flow · kg/s", "Bypass nozzle flow · kg/s"} {
		unit := "kg_s"
		if i == 2 || i == 4 {
			unit = "W"
		}
		residuals.Rows = append(residuals.Rows, DiagnosticRow{Label: label, Cells: []DiagnosticCell{sci(fmt.Sprintf("residual/%d/absolute.%s", i, unit)), sci(fmt.Sprintf("residual/%d/normalized.1", i))}})
	}
	return []DiagnosticTable{power, thrust, nozzles, components, maps, stations, closure, residuals}
}
