package budget

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func admissionException() Exception {
	return Exception{ID: "EX-2026-001", Scope: "route:fixture:/counter/", Metric: "totalBytes", Extra: 2000, ReasonCode: "feature", OwnerRole: "app", ApprovedRole: "reviewer", Issue: 7, ApprovalReview: 11, Expires: "2026-10-09"}
}
func testExceptionProofs(t *testing.T, entries []Exception) []TrustedApproval {
	t.Helper()
	proofs := []TrustedApproval{}
	for _, e := range entries {
		digest, err := ExceptionSHA256(e)
		if err != nil {
			t.Fatal(err)
		}
		proofs = append(proofs, TrustedApproval{e.ApprovalReview, e.ApprovedRole, digest})
	}
	return proofs
}
func testExceptionOptions(t *testing.T, entries ...Exception) ExceptionOptions {
	t.Helper()
	page := PageType{Allocation: Derivation{TotalBytes: 100001, FrameworkBytes: 45000}, Memory: &Memory{GPUInitialBytes: 100001}}
	return ExceptionOptions{File: File{PageTypes: map[string]PageType{"island": page},
		Routes: []RouteRule{{App: "fixture", RouteTemplate: "/counter/", PageTypes: []string{"island"}}}, Exceptions: entries},
		Approvals: testExceptionProofs(t, entries), Now: time.Date(2026, 10, 8, 23, 59, 0, 0, time.UTC)}
}
func TestExceptionExpiryAndExactRoundedDownCaps(t *testing.T) {
	for _, role := range []string{"reviewer", "owner"} {
		days, limit := 30, int64(2000)
		if role == "owner" {
			days, limit = 90, 5000
		}
		for _, delta := range []int64{0, limit, limit + 1} {
			for _, expiryDays := range []int{0, 1, days, days + 1} {
				e := admissionException()
				e.ApprovedRole, e.Extra = role, delta
				e.Expires = time.Date(2026, 10, 8+expiryDays, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
				opts := testExceptionOptions(t, e)
				result, err := EvaluateExceptions(opts)
				valid := delta > 0 && delta <= limit && expiryDays > 0 && expiryDays <= days
				if err != nil || result.Decisions[0].Admitted != valid {
					t.Fatal("exact cap/UTC expiry differs", role, delta, expiryDays, result, err)
				}
			}
		}
	}
	opts := testExceptionOptions(t, admissionException())
	opts.Now = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	result, err := EvaluateExceptions(opts)
	if err != nil || result.Decisions[0].Admitted {
		t.Fatal("expiry did not begin at UTC midnight", result, err)
	}
	opts.Now = time.Date(2026, 10, 8, 17, 0, 0, 0, time.FixedZone("offset", -7*3600))
	result, err = EvaluateExceptions(opts)
	if err != nil || result.Decisions[0].Admitted {
		t.Fatal("local date overrode UTC", result, err)
	}
}
func TestExceptionOverlappingRouteTypeCapsDoNotCompound(t *testing.T) {
	first, second := admissionException(), admissionException()
	first.Extra = 1000
	second.ID, second.Scope, second.Extra = "EX-2026-002", "type:island", 1000
	for _, extra := range []int64{1000, 1001} {
		second.Extra = extra
		opts := testExceptionOptions(t, first, second)
		result, err := EvaluateExceptions(opts)
		valid := extra == 1000
		if err != nil || result.Decisions[0].Admitted != valid || result.Decisions[1].Admitted != valid {
			t.Fatal("overlap compounded allowances", result, err)
		}
	}
	// The broad route also names a smaller goal: both overlapping scopes must use it.
	opts := testExceptionOptions(t, first, second)
	page := opts.File.PageTypes["island"]
	page.Allocation.TotalBytes = 50000
	opts.File.PageTypes["enhanced"] = page
	opts.File.Routes[0].PageTypes = append(opts.File.Routes[0].PageTypes, "enhanced")
	result, err := EvaluateExceptions(opts)
	if err != nil || result.Decisions[0].Admitted || result.Decisions[1].Admitted {
		t.Fatal("smallest applicable allocation was ignored", result, err)
	}
	// Reviewer and owner pools do not let reviewers acquire the owner's share.
	first.Extra, second.Extra, second.ApprovedRole, second.ApprovalReview = 2000, 3000, "owner", 12
	opts = testExceptionOptions(t, first, second)
	result, err = EvaluateExceptions(opts)
	if err != nil || !result.Decisions[0].Admitted || !result.Decisions[1].Admitted {
		t.Fatal("valid mixed-role overlap failed", result, err)
	}
	first.Extra++
	opts = testExceptionOptions(t, first, second)
	result, err = EvaluateExceptions(opts)
	if err != nil || result.Decisions[0].Admitted || result.Decisions[1].Admitted {
		t.Fatal("reviewer exceeded own share", result, err)
	}
}
func TestExceptionNonoverlappingRoutesAreIndependent(t *testing.T) {
	first, second := admissionException(), admissionException()
	second.ID, second.Scope = "EX-2026-002", "route:fixture:/other/"
	opts := testExceptionOptions(t, first, second)
	opts.File.Routes = append(opts.File.Routes, RouteRule{App: "fixture", RouteTemplate: "/other/", PageTypes: []string{"island"}})
	result, err := EvaluateExceptions(opts)
	if err != nil || !result.Decisions[0].Admitted || !result.Decisions[1].Admitted {
		t.Fatal("independent routes combined", result, err)
	}
}
func TestExceptionPolicyNeedsOwnerAndCannotWaiveInvariants(t *testing.T) {
	for _, policy := range []string{"html-compressed", "canonical-build", "served-matches-build", "declared-fetches", "complete-gpu-estimate", "zero-js"} {
		for _, role := range []string{"owner", "reviewer"} {
			e := admissionException()
			e.Metric, e.Policy, e.Extra, e.ApprovedRole = "policy", policy, 0, role
			opts := testExceptionOptions(t, e)
			// A zero-JS exception cannot permit executable assets on static pages.
			page := opts.File.PageTypes["island"]
			opts.File.PageTypes["static"] = page
			opts.File.Routes[0].PageTypes = []string{"static"}
			result, err := EvaluateExceptions(opts)
			valid := policy == "html-compressed" && role == "owner"
			if err != nil || result.Decisions[0].Admitted != valid {
				t.Fatal("protected policy admitted", policy, role, result, err)
			}
		}
	}
	e := admissionException()
	e.Metric, e.Policy, e.Extra, e.ApprovedRole = "policy", "html-compressed", 0, "owner"
	for _, cause := range []string{"over-30-days", "extra-bytes"} {
		changed := e
		switch cause {
		case "over-30-days":
			changed.Expires = "2026-11-08"
		case "extra-bytes":
			changed.Extra = 1
		}
		result, err := EvaluateExceptions(testExceptionOptions(t, changed))
		if err != nil || result.Decisions[0].Admitted {
			t.Fatal("policy exception exceeded admission", cause, result, err)
		}
	}
}
func TestExceptionAllowancePreservesFrameworkAndInput(t *testing.T) {
	first, second := admissionException(), admissionException()
	first.Extra = 1000
	second.ID, second.Metric, second.Extra = "EX-2026-002", "gpuInitialBytes", 2000
	opts := testExceptionOptions(t, first, second)
	before, _ := json.Marshal(opts.File)
	result, err := EvaluateExceptions(opts)
	if err != nil {
		t.Fatal(err)
	}
	allowance := result.Allowance("fixture", "/counter/", "island")
	if allowance.TotalBytes != 1000 || allowance.GPUInitialBytes != 2000 || len(allowance.IDs) != 2 {
		t.Fatal("wrong scoped allowance", allowance)
	}
	allowance.IDs[0] = "changed"
	result.Decisions[0].Admitted = false
	if result.Allowance("fixture", "/counter/", "island").IDs[0] != first.ID {
		t.Fatal("decision/allowance mutation changed private snapshot")
	}
	after, _ := json.Marshal(opts.File)
	if !reflect.DeepEqual(before, after) || opts.File.PageTypes["island"].Allocation.FrameworkBytes != 45000 {
		t.Fatal("exception changed base/share")
	}
	for _, cell := range [][3]string{{"other", "/counter/", "island"}, {"fixture", "/other/", "island"}, {"fixture", "/counter/", "enhanced"}} {
		if got := result.Allowance(cell[0], cell[1], cell[2]); len(got.IDs) != 0 {
			t.Fatal("unregistered cell received allowance", got)
		}
	}
	for _, cause := range []string{"framework", "gpu-incomplete", "base-reduced"} {
		e := first
		opts := testExceptionOptions(t, e)
		page := opts.File.PageTypes["island"]
		switch cause {
		case "framework":
			opts.File.Exceptions[0].Metric = "frameworkBytes"
		case "gpu-incomplete":
			opts.File.Exceptions[0].Metric = "gpuInitialBytes"
			page.Memory = nil
		case "base-reduced":
			page.Allocation.TotalBytes = 10000
		}
		opts.File.PageTypes["island"] = page
		opts.Approvals = testExceptionProofs(t, opts.File.Exceptions)
		result, err := EvaluateExceptions(opts)
		if err != nil || result.Decisions[0].Admitted {
			t.Fatal("forbidden/growing allowance admitted", cause, result, err)
		}
	}
}
func TestExceptionMalformedNativeInputsRemainErrors(t *testing.T) {
	for _, cause := range []string{"duplicate", "scope", "utf8", "policy-field", "unknown-policy", "reason", "datetime", "date", "zero-now", "negative", "limit"} {
		opts := testExceptionOptions(t, admissionException())
		switch cause {
		case "duplicate":
			opts.File.Exceptions = append(opts.File.Exceptions, opts.File.Exceptions[0])
		case "scope":
			opts.File.Exceptions[0].Scope = "route:fixture:/counter/?q=1"
		case "utf8":
			opts.File.Exceptions[0].Scope += string([]byte{0xff})
		case "policy-field":
			opts.File.Exceptions[0].Policy = "html-compressed"
		case "unknown-policy":
			opts.File.Exceptions[0].Metric, opts.File.Exceptions[0].Policy = "policy", "other-policy"
		case "reason":
			opts.File.Exceptions[0].ReasonCode = "ok"
		case "datetime":
			opts.File.Exceptions[0].Expires += "T00:00:00Z"
		case "date":
			opts.File.Exceptions[0].Expires = "2026-02-30"
		case "zero-now":
			opts.Now = time.Time{}
		case "negative":
			opts.File.Exceptions[0].Extra = -1
		case "limit":
			opts.File.Exceptions = make([]Exception, 65)
		}
		_, err := EvaluateExceptions(opts)
		var input *InputError
		if !errors.As(err, &input) || input.Code != "invalid-input" {
			t.Fatal("malformed input became a budget finding", cause, err)
		}
		if cause == "utf8" && input.Pointer != "/exceptions/0/scope" {
			t.Fatal("missing indexed JSON location", input)
		}
	}
	opts := testExceptionOptions(t)
	result, err := EvaluateExceptions(opts)
	if err != nil || result.ApprovalUnavailable || len(result.Decisions) != 0 {
		t.Fatal("empty exception set failed", result, err)
	}
}
