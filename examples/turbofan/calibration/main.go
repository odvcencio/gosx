// Calibration reproduces the fit and all unfitted checks without network access.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"m31labs.dev/gosx/examples/turbofan/cycle"
)

func main() {
	fit := flag.Bool("fit", false, "recompute the approach and idle corrections")
	validatePublic := flag.Bool("validate-public", false, "compare the frozen profile with public speed limits and station references")
	flag.Parse()
	if *fit && *validatePublic {
		fmt.Fprintln(os.Stderr, "--validate-public freezes the existing calibration; run --fit separately")
		os.Exit(2)
	}
	m, err := cycle.CalibrateJT9D()
	if err != nil {
		panic(err)
	}
	if *fit {
		_, err = cycle.FitApproachLoss(m)
		if err != nil {
			panic(err)
		}
		_, err = cycle.FitIdleRecovery(m)
		if err != nil {
			panic(err)
		}
	} else {
		m, err = cycle.JT9D()
		if err != nil {
			panic(err)
		}
	}
	ref := cycle.Reference()
	if *validatePublic {
		report := m.ValidatePublicData()
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, check := range report.Comparisons {
			if check.Status == "limit-exceeded" || check.Status == "speed-incompatible" {
				fmt.Fprintln(os.Stderr, "public reference audit: model speed is incompatible with a reported maximum; see JSON findings")
				os.Exit(1)
			}
		}
		return
	}
	checks := []map[string]any{}
	for _, p := range ref.Points {
		r, err := m.Solve(cycle.MappedInputs{FuelFlowKGS: p.FuelFlowKGS})
		q := map[string]any{"mode": p.Mode, "role": p.Role, "observedFuelKGS": p.FuelFlowKGS, "referenceThrustN": p.ThrustFraction * ref.RatedThrustN}
		if err != nil {
			q["status"] = "unavailable"
			q["reason"] = err.Error()
		} else {
			q["status"] = "converged"
			q["predictedThrustN"] = r.Cycle.NetThrustN
			q["relativeThrustError"] = r.Cycle.NetThrustN/(p.ThrustFraction*ref.RatedThrustN) - 1
			q["result"] = r
		}
		checks = append(checks, q)
	}
	report := map[string]any{"profile": ref.Engine, "uid": ref.UID, "model": m, "checks": checks, "protocol": "Rated thrust/fuel and published OPR/BPR anchor map scaling; approach fits a shared low-speed efficiency correction, and idle fits an additional correction below Nc=0.5. Bounded modeled extensions cover the missing low-speed map region. Climbout is excluded from parameter fitting. Public points were inspected during development; this is an unfitted check, not blind validation."}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
