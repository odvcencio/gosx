package budget

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Explain prints recomputed arithmetic and hypothetical sensitivity without
// editing allocations or presenting unavailable costs as zero.
func Explain(w io.Writer, file File, profile Profile, coefficients Coefficients, pageType string) error {
	for _, input := range []struct {
		definition string
		value      any
	}{{"Budget", file}, {"Profile", profile}, {"Coefficients", coefficients}} {
		if err := validateTyped(input.definition, input.value); err != nil {
			return err
		}
	}
	if err := profile.validate(); err != nil {
		return err
	}
	if err := coefficients.validate(); err != nil {
		return err
	}
	if coefficients.ProfileSHA256 != file.Profile.SHA256 || coefficients.Reference != profile.Reference {
		return errors.New("coefficient profile does not match")
	}
	if err := file.validate(profile, coefficients); err != nil {
		return err
	}
	page, ok := file.PageTypes[pageType]
	if !ok {
		return errors.New("unregistered page type")
	}
	var entries []Coefficient
	for _, s := range coefficients.Sets {
		if s.ID == page.CoefficientSet {
			entries = append([]Coefficient(nil), s.Entries...)
			break
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	var out strings.Builder
	fmt.Fprintf(&out, "%s: network=%s backend=%s reference=%s\ncoefficients: %s\n", pageType, page.Network, page.Backend, profile.Reference, page.CoefficientSet)
	for _, e := range entries {
		value := "null"
		if e.Value != nil {
			value = fmt.Sprint(*e.Value)
		}
		interval := "unavailable"
		if e.CI95[0] != nil {
			interval = fmt.Sprintf("[%d,%d]", *e.CI95[0], *e.CI95[1])
		}
		fmt.Fprintf(&out, "  %s: %s value=%s ci95=%s\n", e.Name, e.Status, value, interval)
	}
	for _, after := range []bool{false, true} {
		label := "cold"
		if after {
			label = "after-ready (1000000us, reused connection)"
		}
		fmt.Fprintf(&out, "%s:\n", label)
		baseline, probes := sensitivity(file, profile, coefficients, pageType, after)
		if baseline == nil {
			out.WriteString("  allocation: unavailable\n")
		} else {
			fmt.Fprintf(&out, "  allocation: %s total=%dB app=%dB framework=%dB reserve=%dB\n", baseline.Status, baseline.TotalBytes, baseline.MinAppBytes, baseline.FrameworkBytes, baseline.AppCriticalReserveBytes)
			for _, s := range baseline.Steps {
				fmt.Fprintf(&out, "    %s: %s/%s %s\n", s.Name, s.Numerator, s.Denominator, s.Unit)
			}
		}
		out.WriteString("  sensitivity (paired counts use their own baseline):\n")
		for _, p := range probes {
			if p.value == nil {
				fmt.Fprintf(&out, "    %s: unavailable\n", p.name)
				continue
			}
			fmt.Fprintf(&out, "    %s: delta=%s B total=%dB app=%dB framework=%dB\n", p.name, p.delta.RatString(), p.value.TotalBytes, p.value.MinAppBytes, p.value.FrameworkBytes)
		}
	}
	_, err := io.WriteString(w, out.String())
	return err
}
