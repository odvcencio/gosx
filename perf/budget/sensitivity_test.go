package budget

import (
	"bytes"
	"errors"
	"math/big"
	"strings"
	"testing"
)

func TestSensitivityWorkedDifferences(t *testing.T) {
	f, p, c := workedFile(t)
	columns := []string{"serverMicros +10%", "jsMicrosPerKB +10%", "wasmCompileMicrosPerRawKB +10%", "overlap +0.1", "fixed work +100000us", "rtt +10%", "down +10%"}
	for name, want := range map[string][7]int64{
		"static":         {-4096, 0, 0, 0, -20480, -18432, 8192},
		"enhanced":       {-2673, -4999, 0, 0, -13364, -12028, 6625},
		"island":         {-3356, -5808, -2032, 2051, -16778, -15100, 31765},
		"engine/js":      {-5732, -53490, 0, 0, -28662, -21834, 14216},
		"engine/shared":  {-9227, -54746, -15758, 16169, -46134, -35144, 41423},
		"go-wasm":        {-11742, -51344, -36072, 37517, -58709, -44723, 84748},
		"video":          {-13433, -57001, 0, 0, -67164, -51164, 74592},
		"scene3d/shared": {-7855, -44847, -8607, 8782, -39275, -29919, 22576},
		"game/shared":    {-7855, -65317, -12535, 12791, -39275, -29919, 35449},
		"preview":        {-6536, -63977, -5795, 5860, -32681, -24896, 23629},
		"scene3d/js":     {-5143, -40347, 0, 0, -25714, -19589, 8311},
		"game/js":        {-5143, -58763, 0, 0, -25714, -19589, 13768},
	} {
		base, probes := sensitivity(f, p, c, name, false)
		if base == nil || len(probes) != 47 {
			t.Fatal("missing sensitivity", name, len(probes))
		}
		byName := make(map[string]sensitivityProbe)
		for _, probe := range probes {
			byName[probe.name] = probe
		}
		for i, key := range columns {
			probe := byName[key]
			if probe.delta == nil {
				t.Fatal("missing finite difference", name, key)
			}
			difference := new(big.Rat).Sub(probe.delta, ratio(want[i], 1))
			difference.Abs(difference)
			if difference.Cmp(ratio(1, 1)) > 0 {
				t.Fatal("wrong finite difference", name, key, probe.delta, want[i])
			}
		}
		if byName["up +10% (startup download)"].delta.Sign() != 0 || byName["jsMicrosPerKB lower"].value != nil {
			t.Fatal("invented uplink effect or coefficient interval")
		}
		if name == "island" {
			js, server := byName[columns[1]].value, byName[columns[0]].value
			if js.TotalBytes != base.TotalBytes-6144 || js.MinAppBytes != base.MinAppBytes-3072 || server.TotalBytes != base.TotalBytes-4096 {
				t.Fatal("probe reused base rounding")
			}
		}
	}
}

func TestSensitivityPairedCounts(t *testing.T) {
	f, p, c := workedFile(t)
	for name := range f.PageTypes {
		for _, after := range []bool{false, true} {
			m, err := newModel(f, p, c, name, after)
			if err != nil {
				t.Fatal(err)
			}
			_, probes := sensitivity(f, p, c, name, after)
			slope := new(big.Rat).Add(ratio(1000000, m.network.DownBytesPerSec), m.slope)
			want := new(big.Rat).Quo(ratio(-10000, 1), slope)
			count := 0
			for _, probe := range probes {
				if !strings.HasPrefix(probe.name, "paired ") {
					continue
				}
				count++
				if probe.delta == nil || probe.delta.Cmp(want) != 0 {
					t.Fatal("extra count hid its nonzero cost", name, after, probe.name, probe.delta, want)
				}
				for _, d := range []*Derivation{probe.base, probe.value} {
					if d.MinAppBytes+d.FrameworkBytes+d.AppCriticalReserveBytes != d.TotalBytes {
						t.Fatal("paired shares do not reconcile")
					}
				}
			}
			if count != 3 {
				t.Fatal("missing paired probes")
			}
		}
	}
	// An unidentified rate with no baseline calls still permits a clearly
	// hypothetical nonzero pair; its coefficient remains null in explanations.
	f, p, c = deriveInputs(t)
	for i := range c.Sets[0].Entries {
		e := &c.Sets[0].Entries[i]
		if e.Name == "pipelineCreateMicros" {
			e.Value = nil
			e.Status = "unknown"
		}
	}
	_, probes := sensitivity(f, p, c, "island", false)
	for _, probe := range probes {
		if probe.name == "paired pipeline +1 @10000us" && probe.delta == nil {
			t.Fatal("unknown inactive rate hid hypothetical probe")
		}
	}
	for i := range c.Sets[0].Entries {
		e := &c.Sets[0].Entries[i]
		if e.Name == "pipelineCreateMicros" {
			zero := int64(0)
			e.Value = &zero
			e.Status = "unused"
			e.Method = "unused"
		}
	}
	_, probes = sensitivity(f, p, c, "island", false)
	for _, probe := range probes {
		if probe.name == "pipelines +1" && probe.value != nil {
			t.Fatal("unused baseline certified an additional call")
		}
	}
}

func TestSensitivityIntervalsAndFractionalNetwork(t *testing.T) {
	f, p, c := deriveInputs(t)
	lo, hi := int64(2000), int64(6000)
	c.Sets[0].Entries[2].CI95 = [2]*int64{&lo, &hi}
	base, probes := sensitivity(f, p, c, "island", false)
	for _, probe := range probes {
		if probe.name == "jsMicrosPerKB lower" && (probe.value == nil || probe.value.TotalBytes <= base.TotalBytes) {
			t.Fatal("lower bound not recomputed")
		}
		if probe.name == "jsMicrosPerKB upper" && (probe.value == nil || probe.value.TotalBytes >= base.TotalBytes) {
			t.Fatal("upper bound not recomputed")
		}
	}
	p.Networks.Slow4G.RTTMicros = 150001
	p.Networks.Slow4G.DownBytesPerSec = 204801
	m, err := newModel(f, p, c, "island", false)
	if err != nil {
		t.Fatal(err)
	}
	m.downOverride = new(big.Rat).Mul(m.down(), ratio(11, 10))
	m.rttOverride = new(big.Rat).Mul(m.rtt(), ratio(11, 10))
	if m.down().IsInt() || m.rtt().IsInt() {
		t.Fatal("fractional network probe was truncated")
	}
	if _, _, err := m.solve(); err != nil {
		t.Fatal(err)
	}
}

type failingExplanationWriter struct{}

func (failingExplanationWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestSensitivityExplain(t *testing.T) {
	f, p, c := deriveInputs(t)
	var first, second bytes.Buffer
	if err := Explain(&first, f, p, c, "island"); err != nil {
		t.Fatal(err)
	}
	if err := Explain(&second, f, p, c, "island"); err != nil || first.String() != second.String() {
		t.Fatal("unstable explanation", err)
	}
	for _, text := range []string{"allocation: illustrative total=438272B", "after-ready (1000000us", "ci95=unavailable", "paired pipeline +1 @10000us", "window:", "unrounded-total:"} {
		if !strings.Contains(first.String(), text) {
			t.Fatal("missing explanation", text)
		}
	}
	if err := Explain(failingExplanationWriter{}, f, p, c, "island"); err == nil {
		t.Fatal("writer error ignored")
	}
	if err := Explain(&second, f, p, c, "unregistered"); err == nil {
		t.Fatal("unregistered type accepted")
	}
	c.Sets[0].Entries[0].Value = nil
	c.Sets[0].Entries[0].Status = "unknown"
	second.Reset()
	if err := Explain(&second, f, p, c, "island"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.String(), "serverMicros: unknown value=null") || !strings.Contains(second.String(), "allocation: unavailable") || strings.Contains(second.String(), "total=0B") {
		t.Fatal("unknown model became zero", second.String())
	}
}
