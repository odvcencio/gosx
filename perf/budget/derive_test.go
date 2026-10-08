package budget

import (
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"
)

func deriveInputs(t *testing.T) (File, Profile, Coefficients) {
	t.Helper()
	f, err := Load("testdata/budget.v2.json", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := LoadProfile("testdata/profile.v1.json", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := LoadCoefficients("testdata/coefficients.v1.json", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return *f, *p, *c
}

func workedFile(t *testing.T) (File, Profile, Coefficients) {
	t.Helper()
	f, p, c := deriveInputs(t)
	base := f.PageTypes["island"]
	f.PageTypes = make(map[string]PageType)
	for _, row := range []struct {
		name, metric, network string
		goal                  float64
		js, wasm, exp         int64
	}{
		{"static", "lcp", "slow4g", 1500, 0, 0, 100},
		{"enhanced", "lcp", "slow4g", 2000, 650000, 0, 100},
		{"island", "island_interactive", "slow4g", 3500, 200000, 730000, 304},
		{"engine/js", "engine_first_tick", "p75", 3500, 650000, 0, 100},
		{"engine/shared", "engine_first_tick", "p75", 3500, 250000, 650000, 343},
		{"go-wasm", "engine_first_tick", "p75", 4000, 120000, 780000, 343},
		{"video", "video_first_frame", "p75", 3000, 150000, 0, 100},
		{"scene3d/shared", "fif", "p75", 3000, 350000, 600000, 343},
		{"game/shared", "fif", "p75", 4000, 350000, 600000, 343},
		{"preview", "enhancement_ready", "p75", 4000, 500000, 450000, 304},
		{"scene3d/js", "fif", "p75", 3000, 750000, 0, 100},
		{"game/js", "fif", "p75", 4000, 750000, 0, 100},
	} {
		page := base
		page.PrimaryMetric = row.metric
		page.Goals = []Goal{{Metric: row.metric, Max: row.goal, Unit: "ms"}}
		page.Network = row.network
		page.Mix = Mix{JSPPM: row.js, WASMPPM: row.wasm, OtherPPM: 1000000 - row.js - row.wasm, WASMExpansionNumerator: row.exp, WASMExpansionDenominator: 100}
		page.Workload = Workload{}
		if row.name == "static" {
			page.AppReserveBytes = 0
		}
		f.PageTypes[row.name] = page
	}
	return f, p, c
}

func TestDeriveWorkedRows(t *testing.T) {
	f, p, c := workedFile(t)
	before, _ := json.Marshal(f)
	derived, err := Derive(f, p, c)
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]struct{ Total, Reserve, App, Framework int64 }
	data, err := os.ReadFile("testdata/derivations.v2.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden) != 12 || len(derived.PageTypes) != 12 {
		t.Fatal("missing worked row")
	}
	for name, want := range golden {
		d := derived.PageTypes[name].Allocation
		if d.TotalBytes != want.Total || d.AppCriticalReserveBytes != want.Reserve || d.MinAppBytes != want.App || d.FrameworkBytes != want.Framework || d.Status != "illustrative" {
			t.Fatalf("%s: %+v", name, d)
		}
		if d.TotalBytes%1024 != 0 || d.FrameworkBytes+d.MinAppBytes+d.AppCriticalReserveBytes != d.TotalBytes {
			t.Fatal("unreconciled allocation", name)
		}
	}
	if err := VerifyDerivation(derived, p, c); err != nil {
		t.Fatal(err)
	}
	exact := derived.PageTypes["island"].Allocation.Steps[5]
	value, ok := new(big.Rat).SetString(exact.Numerator + "/" + exact.Denominator)
	if !ok || value.Cmp(ratio(1045546875000, 2384085)) != 0 {
		t.Fatal("wrong unrounded island solution", exact)
	}
	after, _ := json.Marshal(f)
	if string(before) != string(after) {
		t.Fatal("derive mutated input")
	}
	page := derived.PageTypes["island"]
	page.Goals[0].Max = 1
	derived.PageTypes["island"] = page
	if f.PageTypes["island"].Goals[0].Max != 3500 {
		t.Fatal("result aliases input slices")
	}
}

func TestDeriveRejectAlteredResults(t *testing.T) {
	for name, edit := range map[string]func(*File){
		"byte": func(f *File) { p := f.PageTypes["island"]; p.Allocation.TotalBytes++; f.PageTypes["island"] = p },
		"step": func(f *File) {
			p := f.PageTypes["island"]
			p.Allocation.Steps[0].Numerator = "1"
			f.PageTypes["island"] = p
		},
		"hash": func(f *File) {
			p := f.PageTypes["island"]
			p.Allocation.InputSHA256 = strings.Repeat("f", 64)
			f.PageTypes["island"] = p
		},
		"equivalent-fraction": func(f *File) {
			p := f.PageTypes["island"]
			p.Allocation.Steps[6].Numerator = "876544"
			p.Allocation.Steps[6].Denominator = "2"
			f.PageTypes["island"] = p
		},
		"after-ready": func(f *File) {
			p := f.PageTypes["island"]
			p.AfterReadyAllocation.FrameworkBytes++
			f.PageTypes["island"] = p
		},
		"wire": func(f *File) { p := f.PageTypes["island"]; p.WireEnvelope.TotalBytes++; f.PageTypes["island"] = p },
		"goal": func(f *File) { p := f.PageTypes["island"]; p.Goals[0].Max += 100; f.PageTypes["island"] = p },
		"pin":  func(f *File) { f.Toolchain.SHA256 = strings.Repeat("f", 64) },
		"metric": func(f *File) {
			p := f.PageTypes["island"]
			p.PrimaryMetric = "lcp"
			p.Goals[0].Metric = "lcp"
			f.PageTypes["island"] = p
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, p, c := deriveInputs(t)
			f, err := Derive(f, p, c)
			if err != nil {
				t.Fatal(err)
			}
			edit(&f)
			before, _ := json.Marshal(f)
			if VerifyDerivation(f, p, c) == nil {
				t.Fatal("altered result accepted")
			}
			after, _ := json.Marshal(f)
			if string(before) != string(after) {
				t.Fatal("verify changed the cap")
			}
		})
	}
}

func TestDeriveLimitsAndUnknowns(t *testing.T) {
	for name, edit := range map[string]func(*File, *Coefficients){
		"negative-window": func(f *File, _ *Coefficients) {
			p := f.PageTypes["island"]
			p.Goals[0].Max = 100
			f.PageTypes["island"] = p
		},
		"search-limit": func(f *File, _ *Coefficients) {
			p := f.PageTypes["island"]
			p.Goals[0].Max = 1e9
			f.PageTypes["island"] = p
		},
		"critical-reserve": func(f *File, _ *Coefficients) {
			p := f.PageTypes["island"]
			p.AppReserveBytes = 1 << 30
			f.PageTypes["island"] = p
		},
		"unknown-active": func(_ *File, c *Coefficients) {
			e := &c.Sets[0].Entries[3]
			e.Status = "unknown"
			e.Value = nil
			e.Method = "prior"
		},
		"unknown-fixed": func(_ *File, c *Coefficients) {
			e := &c.Sets[0].Entries[0]
			e.Status = "unknown"
			e.Value = nil
			e.Method = "prior"
		},
		"overflow-fraction": func(f *File, _ *Coefficients) {
			p := f.PageTypes["island"]
			p.Mix.WASMExpansionNumerator = -1
			f.PageTypes["island"] = p
		},
		"mix-total":        func(f *File, _ *Coefficients) { p := f.PageTypes["island"]; p.Mix.JSPPM++; f.PageTypes["island"] = p },
		"missing-cold":     func(_ *File, c *Coefficients) { c.Sets[0].Scenario = "soft" },
		"backend-mismatch": func(_ *File, c *Coefficients) { c.Sets[0].Backend = "webgpu" },
	} {
		t.Run(name, func(t *testing.T) {
			f, p, c := deriveInputs(t)
			edit(&f, &c)
			if _, err := Derive(f, p, c); err == nil {
				t.Fatal("invalid model accepted")
			}
		})
	}
}

func TestDeriveMeasuredEndpoints(t *testing.T) {
	f, p, c := deriveInputs(t)
	zero := int64(0)
	c.Sets[0].PredictionErrorPPM = &zero
	for i := range c.Sets[0].Entries {
		e := &c.Sets[0].Entries[i]
		e.Status = "measured"
		e.Method = "isolated-fit"
		e.CI95 = [2]*int64{e.Value, e.Value}
		e.NVisits = 30
		e.NBlocks = 2
	}
	for i := range c.Sets[0].Entries {
		e := &c.Sets[0].Entries[i]
		if e.Name == "jsMicrosPerKB" {
			lo, hi := int64(2000), int64(6000)
			e.CI95 = [2]*int64{&lo, &hi}
		}
	}
	derived, err := Derive(f, p, c)
	if err != nil {
		t.Fatal(err)
	}
	if d := derived.PageTypes["island"].Allocation; d.Status != "proxy-measured" || d.TotalBytes >= 438272 {
		t.Fatal("measured costs did not use conservative endpoints", d)
	}
	model, err := newModel(f, p, c, "island", false)
	if err != nil {
		t.Fatal(err)
	}
	if model.slope.Cmp(new(big.Rat).SetFrac64(14774, 10000)) != 0 {
		t.Fatal("wrong upper-endpoint slope", model.slope)
	}
}
