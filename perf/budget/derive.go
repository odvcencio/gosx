package budget

import (
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"strconv"
)

func validateTyped(definition string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New("invalid typed budget input")
	}
	var decoded json.RawMessage
	return decodeInput(data, definition, &decoded)
}

// Derive returns new allocations without mutating the inputs. Priors and pilots
// remain illustrative; measured costs use upper endpoints and overlap its lower.
func Derive(file File, profile Profile, coefficients Coefficients) (File, error) {
	for _, input := range []struct {
		definition string
		value      any
	}{
		{"Budget", file}, {"Profile", profile}, {"Coefficients", coefficients},
	} {
		if err := validateTyped(input.definition, input.value); err != nil {
			return File{}, err
		}
	}
	if err := profile.validate(); err != nil {
		return File{}, err
	}
	if err := coefficients.validate(); err != nil {
		return File{}, err
	}
	if coefficients.ProfileSHA256 != file.Profile.SHA256 || coefficients.Reference != profile.Reference {
		return File{}, errors.New("coefficient profile does not match")
	}
	// Decode a copy so every map, slice and coefficient pointer remains independent.
	data, _ := json.Marshal(file)
	var result File
	if err := json.Unmarshal(data, &result); err != nil {
		return File{}, errors.New("cannot copy budget input")
	}
	for name, page := range result.PageTypes {
		cold, err := newModel(file, profile, coefficients, name, false)
		if err != nil {
			return File{}, err
		}
		page.Allocation, err = cold.allocation()
		if err != nil {
			return File{}, err
		}
		after, err := newModel(file, profile, coefficients, name, true)
		if err != nil {
			return File{}, err
		}
		page.AfterReadyAllocation, err = after.allocation()
		if err != nil {
			return File{}, err
		}
		// Other hosting encodings need verified body expansion ratios from collection.
		if page.WireEnvelope.Compressor != "canonical" {
			return File{}, errors.New("hosting envelope requires measured encoding")
		}
		page.WireEnvelope.TotalBytes = page.Allocation.TotalBytes
		result.PageTypes[name] = page
	}
	if err := result.validate(profile, coefficients); err != nil {
		return File{}, err
	}
	return result, nil
}

// VerifyDerivation compares exact steps, allocations, status and input hashes.
// It never adopts a higher allowance when an input changes.
func VerifyDerivation(file File, profile Profile, coefficients Coefficients) error {
	derived, err := Derive(file, profile, coefficients)
	if err != nil {
		return err
	}
	for name, page := range file.PageTypes {
		want := derived.PageTypes[name]
		if !reflect.DeepEqual(page.Allocation, want.Allocation) || !reflect.DeepEqual(page.AfterReadyAllocation, want.AfterReadyAllocation) || page.WireEnvelope != want.WireEnvelope {
			return errors.New("derivation does not match inputs")
		}
	}
	return nil
}

func newModel(file File, profile Profile, coefficients Coefficients, name string, after bool) (planningModel, error) {
	page, ok := file.PageTypes[name]
	if !ok || !knownPageType(name) {
		return planningModel{}, errors.New("unregistered page type")
	}
	var selected *CoefficientSet
	for i := range coefficients.Sets {
		s := &coefficients.Sets[i]
		if s.ID == page.CoefficientSet {
			selected = s
			break
		}
	}
	if selected == nil || selected.Backend != page.Backend || selected.Scenario != "hard-cold" {
		return planningModel{}, errors.New("page requires matching cold coefficients")
	}
	if page.Mix.JSPPM+page.Mix.WASMPPM+page.Mix.ProgramPPM+page.Mix.OtherPPM != 1000000 || page.MinAppPPM < 500000 || page.MinAppPPM > 1000000 {
		return planningModel{}, errors.New("invalid byte mix or app share")
	}
	network := profile.Networks.Slow4G
	if page.Network == "p75" {
		network = profile.Networks.P75
	} else if page.Network != "slow4g" {
		return planningModel{}, errors.New("unknown network")
	}
	goal := new(big.Rat)
	for _, g := range page.Goals {
		if g.Metric == page.PrimaryMetric {
			if g.Unit != "ms" || metricUnit(g.Metric) != "ms" {
				return planningModel{}, errors.New("primary goal must be a duration")
			}
			if _, ok := goal.SetString(strconv.FormatFloat(g.Max, 'g', -1, 64)); !ok {
				return planningModel{}, errors.New("invalid goal")
			}
			goal.Mul(goal, ratio(1000, 1))
		}
	}
	setup, first := profile.SetupRTTs, profile.FirstByteRTTs
	work, reserve := page.Workload, page.AppReserveBytes
	if after {
		goal.SetInt64(1000000)
		setup = 0
		first = 1
		work = page.AfterReadyWorkload
		reserve = 0
	}
	status := "proxy-measured"
	costs := make(map[string]*big.Rat)
	for _, e := range selected.Entries {
		used := coefficientUsed(e.Name, page.Mix, work)
		if !used {
			costs[e.Name] = new(big.Rat)
			continue
		}
		if e.Value == nil || e.Status == "unused" {
			return planningModel{}, errors.New("required coefficient is unavailable")
		}
		value := *e.Value
		if e.Status == "measured" {
			value = *e.CI95[1]
			if e.Name == "wasmOverlapPPM" {
				value = *e.CI95[0]
			}
		} else {
			status = "illustrative"
		}
		costs[e.Name] = ratio(value, 1)
	}
	if status != "illustrative" && profile.Reference != "desktop-cpu-proxy" {
		return planningModel{}, errors.New("phone certification is unavailable")
	}
	window := new(big.Rat).Sub(goal, new(big.Rat).Mul(ratio(int64(setup+first), 1), ratio(network.RTTMicros, 1)))
	window.Sub(window, costs["serverMicros"])
	window.Sub(window, costs["renderMicros"])
	slope := new(big.Rat).Mul(ratio(page.Mix.JSPPM, 1000000000), costs["jsMicrosPerKB"])
	wasm := new(big.Rat).Mul(ratio(page.Mix.WASMPPM, 1000000000), costs["wasmCompileMicrosPerRawKB"])
	wasm.Mul(wasm, ratio(page.Mix.WASMExpansionNumerator, page.Mix.WASMExpansionDenominator))
	overlap := new(big.Rat).Quo(costs["wasmOverlapPPM"], ratio(1000000, 1))
	wasm.Mul(wasm, new(big.Rat).Sub(ratio(1, 1), overlap))
	slope.Add(slope, wasm)
	slope.Add(slope, new(big.Rat).Mul(ratio(page.Mix.ProgramPPM, 1000000000), costs["programDecodeMicrosPerKB"]))
	fixed := new(big.Rat)
	for _, term := range []struct {
		name  string
		count int64
	}{
		{"wasmInstantiateMicros", work.WASMModules}, {"engineStartMicros", work.Engines},
		{"pipelineCreateMicros", work.Pipelines}, {"shaderCompileMicrosPerKB", work.ShaderKB}, {"hydrationMicrosPerIsland", work.Islands},
	} {
		fixed.Add(fixed, new(big.Rat).Mul(ratio(term.count, 1), costs[term.name]))
	}
	// Bind the selected assumptions and exact typed inputs as well as raw-file refs.
	fingerprint, _ := json.Marshal(struct {
		Algorithm, Name string
		After           bool
		Refs            []Ref
		Profile         Profile
		Coefficients    Coefficients
		Network         string
		Goal            *big.Rat
		Mix             Mix
		Workload        Workload
		Reserve, Share  int64
		Set, Backend    string
	}{"transfer-cpu/v1", name, after, []Ref{file.Profile, file.Coefficients, file.Toolchain, file.Fixtures}, profile, coefficients,
		page.Network, goal, page.Mix, work, reserve, page.MinAppPPM, page.CoefficientSet, page.Backend})
	return planningModel{network: network, initial: profile.InitCwndBytes, quantum: profile.QuantumBytes, reserve: reserve,
		share: page.MinAppPPM, static: name == "static", window: window, slope: slope, fixed: fixed, status: status, fingerprint: inputDigest(fingerprint)}, nil
}
