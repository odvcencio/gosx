package budget

import "math/big"

// Probe results are private explanation values, not measurement records.
type sensitivityProbe struct {
	name        string
	base, value *Derivation
	delta       *big.Rat
}

func selectedValue(e Coefficient) *big.Rat {
	if e.Value == nil {
		return nil
	}
	value := *e.Value
	if e.Status == "measured" {
		value = *e.CI95[1]
		if e.Name == "wasmOverlapPPM" {
			value = *e.CI95[0]
		}
	}
	return ratio(value, 1)
}

func evaluateProbe(name string, base, changed planningModel) sensitivityProbe {
	p := sensitivityProbe{name: name}
	x, err := base.allocation()
	if err != nil {
		return p
	}
	y, err := changed.allocation()
	if err != nil {
		return p
	}
	a, _ := new(big.Rat).SetString(x.Steps[5].Numerator + "/" + x.Steps[5].Denominator)
	b, _ := new(big.Rat).SetString(y.Steps[5].Numerator + "/" + y.Steps[5].Denominator)
	p.base = &x
	p.value = &y
	p.delta = new(big.Rat).Sub(b, a)
	return p
}

func sensitivity(file File, profile Profile, coefficients Coefficients, name string, after bool) (*Derivation, []sensitivityProbe) {
	page := file.PageTypes[name]
	var entries []Coefficient
	for _, s := range coefficients.Sets {
		if s.ID == page.CoefficientSet {
			entries = s.Entries
			break
		}
	}
	base, baseErr := newModel(file, profile, coefficients, name, after)
	var baseline *Derivation
	if baseErr == nil {
		if d, err := base.allocation(); err == nil {
			baseline = &d
		}
	}
	var probes []sensitivityProbe
	add := func(label string, m planningModel, err error) {
		p := sensitivityProbe{name: label}
		if baseline != nil && err == nil {
			p = evaluateProbe(label, base, m)
		}
		probes = append(probes, p)
	}
	entriesByName := make(map[string]Coefficient)
	for _, e := range entries {
		entriesByName[e.Name] = e
		for i, label := range []string{" lower", " upper", " +10%"} {
			var value *big.Rat
			if i < 2 && e.CI95[i] != nil {
				value = ratio(*e.CI95[i], 1)
			}
			if i == 2 && e.Value != nil {
				value = new(big.Rat).Mul(selectedValue(e), ratio(11, 10))
			}
			if value == nil {
				probes = append(probes, sensitivityProbe{name: e.Name + label})
				continue
			}
			m, err := modelWithCosts(file, profile, coefficients, name, after, map[string]*big.Rat{e.Name: value})
			add(e.Name+label, m, err)
		}
	}
	// Fractional network changes remain rational instead of rounding to us or B/s.
	if baseline != nil {
		m := base
		m.downOverride = new(big.Rat).Mul(base.down(), ratio(11, 10))
		add("down +10%", m, nil)
		m = base
		m.rttOverride = new(big.Rat).Mul(base.rtt(), ratio(11, 10))
		rtts := int64(profile.SetupRTTs + profile.FirstByteRTTs)
		if after {
			rtts = 1
		}
		m.window = new(big.Rat).Sub(base.window, new(big.Rat).Mul(new(big.Rat).Sub(m.rtt(), base.rtt()), ratio(rtts, 1)))
		add("rtt +10%", m, nil)
		add("up +10% (startup download)", base, nil)
		m = base
		m.fixed = new(big.Rat).Add(base.fixed, ratio(100000, 1))
		add("fixed work +100000us", m, nil)
	} else {
		for _, label := range []string{"down +10%", "rtt +10%", "up +10% (startup download)", "fixed work +100000us"} {
			probes = append(probes, sensitivityProbe{name: label})
		}
	}
	// Expansion and compile rate multiply the same raw-byte term.
	for _, probe := range []struct {
		label, key string
		overlap    bool
	}{
		{"wasm expansion +10%", "wasmCompileMicrosPerRawKB", false}, {"overlap +0.1", "wasmOverlapPPM", true},
	} {
		value := selectedValue(entriesByName[probe.key])
		if value == nil {
			probes = append(probes, sensitivityProbe{name: probe.label})
			continue
		}
		if probe.overlap {
			value.Add(value, ratio(100000, 1))
			if value.Cmp(ratio(1000000, 1)) > 0 {
				value.SetInt64(1000000)
			}
		} else {
			value.Mul(value, ratio(11, 10))
		}
		m, err := modelWithCosts(file, profile, coefficients, name, after, map[string]*big.Rat{probe.key: value})
		add(probe.label, m, err)
	}
	work := page.Workload
	if after {
		work = page.AfterReadyWorkload
	}
	for _, term := range []struct {
		label, key string
		count      int64
	}{
		{"wasmModules +1", "wasmInstantiateMicros", work.WASMModules}, {"engines +1", "engineStartMicros", work.Engines},
		{"pipelines +1", "pipelineCreateMicros", work.Pipelines}, {"shaderKB +1", "shaderCompileMicrosPerKB", work.ShaderKB},
		{"islands +1", "hydrationMicrosPerIsland", work.Islands},
	} {
		entry := entriesByName[term.key]
		value := selectedValue(entry)
		if baseline == nil || value == nil || entry.Status == "unused" {
			probes = append(probes, sensitivityProbe{name: term.label})
			continue
		}
		m := base
		m.fixed = new(big.Rat).Add(base.fixed, value)
		add(term.label, m, nil)
	}
	// Compare each extra count to its own nonzero-cost baseline, so existing
	// counts do not get charged again as if they were the added count.
	for _, term := range []struct {
		label, key         string
		count, rate, extra int64
	}{
		{"paired pipeline +1 @10000us", "pipelineCreateMicros", work.Pipelines, 10000, 1},
		{"paired island +1 @10000us", "hydrationMicrosPerIsland", work.Islands, 10000, 1},
		{"paired shaderKB +10 @1000us/KB", "shaderCompileMicrosPerKB", work.ShaderKB, 1000, 10},
	} {
		p := sensitivityProbe{name: term.label}
		value := selectedValue(entriesByName[term.key])
		if value == nil && term.count == 0 {
			value = new(big.Rat)
		} // Empty baseline product; the hypothetical rate stays explicit.
		if baseline != nil && value != nil {
			paired := base
			removed := new(big.Rat).Mul(value, ratio(term.count, 1))
			paired.fixed = new(big.Rat).Sub(base.fixed, removed)
			paired.fixed.Add(paired.fixed, new(big.Rat).Mul(ratio(term.rate, 1), ratio(term.count, 1)))
			changed := paired
			changed.fixed = new(big.Rat).Add(paired.fixed, new(big.Rat).Mul(ratio(term.rate, 1), ratio(term.extra, 1)))
			p = evaluateProbe(term.label, paired, changed)
		}
		probes = append(probes, p)
	}
	return baseline, probes
}
