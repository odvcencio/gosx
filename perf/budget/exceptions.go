package budget

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

type ExceptionOptions struct {
	File      File
	Approvals []TrustedApproval
	Now       time.Time
}

// ExceptionDecision contains no free-form diagnostic or reviewer identity.
type ExceptionDecision struct {
	ID       string
	Admitted bool
}

// ExceptionSet is a private decision snapshot, not another public JSON root.
// Missing trusted evidence cannot certify enforcement; callers must retain
// the exception failure and use report-only mode when ApprovalUnavailable.
type ExceptionSet struct {
	Decisions           []ExceptionDecision
	ApprovalUnavailable bool
	admitted            []Exception
	cells               map[string]bool
}

type ExceptionAllowance struct {
	TotalBytes, GPUInitialBytes int64
	Policies, IDs               []string
}

// EvaluateExceptions admits bounded, unexpired, content-bound allowances.
// Rejected allowances are decisions; malformed inputs remain typed errors.
// No allowance changes the base allocation or grants framework bytes.
func EvaluateExceptions(opts ExceptionOptions) (*ExceptionSet, error) {
	if opts.Now.IsZero() || len(opts.File.Exceptions) > 64 {
		return nil, exceptionInput("")
	}
	proofs, err := approvalIndex(opts.Approvals)
	if err != nil {
		return nil, err
	}
	now := opts.Now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	out := &ExceptionSet{Decisions: []ExceptionDecision{}, admitted: []Exception{}, cells: map[string]bool{}}
	for _, route := range opts.File.Routes {
		for _, name := range route.PageTypes {
			out.cells[strings.Join([]string{route.App, route.RouteTemplate, name}, "|")] = true
		}
	}
	out.ApprovalUnavailable = len(opts.File.Exceptions) > 0 && len(proofs) == 0
	seen := map[string]bool{}
	bases := make([]int64, len(opts.File.Exceptions))
	types := make([][]string, len(opts.File.Exceptions))
	valid := make([]bool, len(opts.File.Exceptions))
	for i, e := range opts.File.Exceptions {
		pointer := "/exceptions/" + strconv.Itoa(i)
		if err := validateExceptionShape(e); err != nil {
			input := err.(*InputError)
			return nil, exceptionInput(pointer + input.Pointer)
		}
		if seen[e.ID] || !exceptionReason(e.ReasonCode) || (e.Metric == "policy") != (e.Policy != "") {
			return nil, exceptionInput(pointer)
		}
		seen[e.ID] = true
		types[i], err = exceptionTypes(opts.File, e.Scope)
		if err != nil {
			return nil, exceptionInput(pointer + "/scope")
		}
		base, ok := exceptionBase(opts.File, e, types[i])
		bases[i] = base
		digest, err := ExceptionSHA256(e)
		if err != nil {
			return nil, err
		}
		expiry, _ := time.Parse("2006-01-02", e.Expires) // Shape validation checked the date.
		days, ppm := 30, int64(20000)
		if e.ApprovedRole == "owner" {
			days, ppm = 90, 50000
		}
		if e.Metric == "policy" {
			days = 30
			ok = e.ApprovedRole == "owner" && e.Extra == 0 && exceptionPolicyAllowed(opts.File, e.Policy, types[i])
		} else {
			ok = ok && e.Extra > 0 && e.Extra <= exceptionFraction(base, ppm)
		}
		valid[i] = ok && expiry.After(today) && !expiry.After(today.AddDate(0, 0, days)) && proofs[TrustedApproval{e.ApprovalReview, e.ApprovedRole, digest}]
	}
	// Compare each overlapping set once against its smallest base. Percentages
	// never compound. Reviewer allowances also stay within their own 2% pool
	// when an owner allowance raises the combined ceiling to 5%.
	for i, e := range opts.File.Exceptions {
		if e.Metric == "policy" || e.Metric == "frameworkBytes" {
			continue
		}
		for _, name := range types[i] {
			for _, scope := range exceptionCells(opts.File, e.Scope, name) {
				var overlapping []int
				base, total, reviewers, ppm := bases[i], int64(0), int64(0), int64(20000)
				for j, other := range opts.File.Exceptions {
					if other.Metric != e.Metric || !exceptionApplies(other.Scope, scope[0], scope[1], name) {
						continue
					}
					overlapping = append(overlapping, j)
					base = min(base, bases[j])
					total += other.Extra // At most 64 schema-bounded integers: no int64 overflow.
					if other.ApprovedRole == "owner" {
						ppm = 50000
					} else {
						reviewers += other.Extra
					}
				}
				if total > exceptionFraction(base, ppm) || reviewers > exceptionFraction(base, 20000) {
					for _, j := range overlapping {
						valid[j] = false
					}
				}
			}
		}
	}
	for i, e := range opts.File.Exceptions {
		out.Decisions = append(out.Decisions, ExceptionDecision{e.ID, valid[i]})
		if valid[i] {
			out.admitted = append(out.admitted, e)
		}
	}
	return out, nil
}

func exceptionInput(pointer string) error {
	return &InputError{Code: "invalid-input", Reference: "exceptions", Pointer: pointer}
}
func exceptionReason(reason string) bool {
	switch reason {
	case "feature", "correctness", "temporary", "rebaseline", "optimization":
		return true
	}
	return false
}
func exceptionTypes(file File, scope string) ([]string, error) {
	kind, target, ok := strings.Cut(scope, ":")
	if kind == "type" && ok {
		if _, configured := file.PageTypes[target]; configured && knownPageType(target) {
			return []string{target}, nil
		}
	}
	if kind == "route" && ok {
		app, route, parsed := strings.Cut(target, ":")
		if parsed && validRoute(route) {
			for _, rule := range file.Routes {
				if rule.App == app && rule.RouteTemplate == route && len(rule.PageTypes) > 0 {
					names := append([]string{}, rule.PageTypes...)
					sort.Strings(names)
					for _, name := range names {
						if _, configured := file.PageTypes[name]; !configured || !knownPageType(name) {
							return nil, exceptionInput("/scope")
						}
					}
					return names, nil
				}
			}
		}
	}
	return nil, exceptionInput("/scope")
}
func exceptionBase(file File, e Exception, types []string) (int64, bool) {
	base := int64(9007199254740991)
	for _, name := range types {
		page := file.PageTypes[name]
		n := page.Allocation.TotalBytes
		if e.Metric == "gpuInitialBytes" {
			if page.Memory == nil {
				return 0, false
			}
			n = page.Memory.GPUInitialBytes
		}
		if n < 0 || n > 9007199254740991 {
			return 0, false
		}
		base = min(base, n)
	}
	return base, e.Metric != "frameworkBytes"
}
func exceptionFraction(base, ppm int64) int64 {
	return base/1000000*ppm + base%1000000*ppm/1000000
}
func exceptionPolicyAllowed(file File, policy string, types []string) bool {
	props := inputDefinitions["PageType"].(map[string]any)["properties"].(map[string]any)
	if validateInput(policy, props["requiredPolicies"].(map[string]any)["items"]) != nil {
		return false
	}
	switch policy {
	case "canonical-build", "served-matches-build", "declared-fetches", "complete-gpu-estimate":
		return false
	}
	for _, name := range types {
		family, _, _ := pageTypeVariant(name)
		if policy == "zero-js" && family == "static" {
			return false
		}
	}
	return true
}
func exceptionApplies(scope, app, route, pageType string) bool {
	return scope == "type:"+pageType || scope == "route:"+app+":"+route
}
func exceptionCells(file File, scope, name string) [][2]string {
	cells := [][2]string{}
	for _, rule := range file.Routes {
		for _, pageType := range rule.PageTypes {
			if name == pageType && exceptionApplies(scope, rule.App, rule.RouteTemplate, name) {
				cells = append(cells, [2]string{rule.App, rule.RouteTemplate})
			}
		}
	}
	if len(cells) == 0 { // A configured type may precede its route registration.
		cells = append(cells, [2]string{})
	}
	return cells
}

// Allowance returns only admitted additions for an exact route/type. Total
// additions belong entirely to app bytes; framework ceilings stay unchanged.
func (set *ExceptionSet) Allowance(app, route, pageType string) ExceptionAllowance {
	out := ExceptionAllowance{Policies: []string{}, IDs: []string{}}
	if set == nil || !set.cells[strings.Join([]string{app, route, pageType}, "|")] {
		return out
	}
	for _, e := range set.admitted {
		if !exceptionApplies(e.Scope, app, route, pageType) {
			continue
		}
		switch e.Metric {
		case "totalBytes":
			out.TotalBytes += e.Extra
		case "gpuInitialBytes":
			out.GPUInitialBytes += e.Extra
		case "policy":
			out.Policies = append(out.Policies, e.Policy)
		}
		out.IDs = append(out.IDs, e.ID)
	}
	sort.Strings(out.Policies)
	sort.Strings(out.IDs)
	return out
}
