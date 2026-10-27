package cycle

import (
	"fmt"
	"strings"
)

type PublicLimitView struct {
	ID, Label, Rotor, Limit, Model, Difference, Status string
}

type PublicComparisonView struct {
	ID, Label, Category, Status, Reference, Prediction, Difference, Conditions, Finding, Source, SourceURL string
}

type PublicComparisonGroup struct {
	Title string
	Note  string
	Open  bool
	Rows  []PublicComparisonView
}

func PublicLimitViews(d EngineeringRecord) []PublicLimitView {
	var rows []PublicLimitView
	for _, limit := range publicData().Limits {
		p := "public/" + limit.ID + "/"
		row := PublicLimitView{ID: limit.ID, Label: limit.Variant, Rotor: strings.ToUpper(limit.Quantity), Limit: fmt.Sprintf("%.0f", limit.MaximumRPM), Model: "—", Difference: "—", Status: "—"}
		if value, ok := d.Numbers[p+"speed.rpm"]; ok {
			row.Model = fmt.Sprintf("%.1f", value)
			row.Difference = fmt.Sprintf("%+.1f", d.Numbers[p+"difference.rpm"])
			row.Status = d.Text[p+"status"]
		}
		rows = append(rows, row)
	}
	return rows
}

func PublicComparisonGroups(report PublicValidationReport) []PublicComparisonGroup {
	groups := []PublicComparisonGroup{{Title: "Same-variant hot-section specifications · qualified comparison", Open: true}, {Title: "Nominal idle speeds · operating conditions missing"}, {Title: "NASA JT9D-7A station references · earlier variant"}, {Title: "FAA takeoff exhaust-temperature maxima · limits"}}
	for _, row := range report.Comparisons {
		group := 0
		switch row.Category {
		case "speed limit":
			continue
		case "exhaust temperature limit":
			group = 3
		case "nominal reference":
			group = 1
		case "earlier-variant reference":
			group = 2
		}
		v := PublicComparisonView{ID: row.ID, Label: row.Label, Category: row.Category, Status: row.Status, Reference: fmt.Sprintf("%.2f %s", row.Reference, row.Unit), Prediction: "—", Difference: "—", Conditions: row.Conditions, Finding: row.Finding, Source: row.Source}
		if row.Prediction != nil {
			v.Prediction = fmt.Sprintf("%.2f %s", *row.Prediction, row.Unit)
		}
		if row.RelativeDifference != nil {
			v.Difference = fmt.Sprintf("%+.2f%%", *row.RelativeDifference*100)
		}
		if row.Command != nil {
			v.Conditions += fmt.Sprintf("; model fuel %.5f kg/s", row.Command.FuelFlowKGS)
		}
		if row.Category == "published specification" {
			groups[group].Note = "The exhibit provides specifications without sensor locations or uncertainty. Station mapping is provisional, fuel is beyond the UI interval, and these differences do not establish measured engine accuracy. No parameters were refitted."
			if row.Status == "speed-incompatible" {
				groups[group].Note = fmt.Sprintf("This point violates reported speed bounds (model N1 %.1f, N2 %.1f rpm). ", row.N1RPM, row.N2RPM) + groups[group].Note
			}
			v.Finding = "Provisional mapping: combustor outlet to model station 4."
			if row.ID == "hot-turbine-inlet" {
				v.Finding = "Provisional mapping: turbine inlet to model station 41 after the first cooling mix."
			}
		}
		for _, source := range report.Sources {
			if source.ID == row.Source {
				v.SourceURL = source.URL
			}
		}
		groups[group].Rows = append(groups[group].Rows, v)
	}
	return groups
}
