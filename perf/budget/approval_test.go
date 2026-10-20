package budget

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestApprovalCanonicalContentExcludesOnlyReviewID(t *testing.T) {
	e := admissionException()
	digest, err := ExceptionSHA256(e)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"approvedRole":"reviewer","expires":"2026-10-09","extra":2000,"id":"EX-2026-001","issue":7,"metric":"totalBytes","ownerRole":"app","reasonCode":"feature","scope":"route:fixture:/counter/"}` + "\n"
	sum := sha256.Sum256([]byte(expected))
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatal("hash is not canonical reviewed content")
	}
	e.ApprovalReview++
	if actual, err := ExceptionSHA256(e); err != nil || actual != digest {
		t.Fatal("review ID changed content hash", err)
	}
	for name, edit := range map[string]func(*Exception){
		"id":            func(e *Exception) { e.ID = "EX-2026-002" },
		"scope":         func(e *Exception) { e.Scope = "type:island" },
		"metric":        func(e *Exception) { e.Metric = "gpuInitialBytes" },
		"extra":         func(e *Exception) { e.Extra-- },
		"policy":        func(e *Exception) { e.Policy = "html-compressed" },
		"reason":        func(e *Exception) { e.ReasonCode = "optimization" },
		"owner-role":    func(e *Exception) { e.OwnerRole = "runtime" },
		"approved-role": func(e *Exception) { e.ApprovedRole = "owner" },
		"issue":         func(e *Exception) { e.Issue++ },
		"expiry":        func(e *Exception) { e.Expires = "2026-10-10" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := admissionException()
			edit(&changed)
			actual, err := ExceptionSHA256(changed)
			if err != nil || actual == digest {
				t.Fatal("content change did not invalidate hash", err)
			}
		})
	}
}

func TestApprovalProofRequiresExactRoleReviewAndContent(t *testing.T) {
	for _, cause := range []string{"valid", "missing", "role", "review", "content", "changed-issue"} {
		t.Run(cause, func(t *testing.T) {
			opts := testExceptionOptions(t, admissionException())
			switch cause {
			case "missing":
				opts.Approvals = nil
			case "role":
				opts.Approvals[0].Role = "owner"
			case "review":
				opts.Approvals[0].ReviewID++
			case "content":
				opts.Approvals[0].ExceptionSHA256 = strings.Repeat("0", 64)
			case "changed-issue":
				opts.File.Exceptions[0].Issue++
			}
			result, err := EvaluateExceptions(opts)
			if err != nil || result.Decisions[0].Admitted != (cause == "valid") {
				t.Fatal("unbound approval admitted", result, err)
			}
			if result.ApprovalUnavailable != (cause == "missing") {
				t.Fatal("missing trusted proof not distinguished")
			}
		})
	}
}

func TestApprovalRejectsMalformedOrConflictingEvidence(t *testing.T) {
	good := testExceptionOptions(t, admissionException()).Approvals[0]
	for _, proofs := range [][]TrustedApproval{
		{{ReviewID: 0, Role: good.Role, ExceptionSHA256: good.ExceptionSHA256}},
		{{ReviewID: 1, Role: "author", ExceptionSHA256: good.ExceptionSHA256}},
		{{ReviewID: 1, Role: good.Role, ExceptionSHA256: "invalid"}},
		{good, good},
		{good, {ReviewID: good.ReviewID, Role: "owner", ExceptionSHA256: strings.Repeat("1", 64)}},
		make([]TrustedApproval, 65),
	} {
		opts := testExceptionOptions(t, admissionException())
		opts.Approvals = proofs
		_, err := EvaluateExceptions(opts)
		var input *InputError
		if !errors.As(err, &input) || input.Reference != "approvals" || input.Code != "invalid-input" {
			t.Fatal("malformed proof was a budget decision", err)
		}
	}
	// One approved review can bind distinct exceptions, without exposing an account.
	opts := testExceptionOptions(t, admissionException(), admissionException())
	opts.File.Exceptions[1].ID = "EX-2026-002"
	opts.File.Exceptions[0].Extra, opts.File.Exceptions[1].Extra = 1000, 1000
	opts.Approvals = testExceptionProofs(t, opts.File.Exceptions)
	result, err := EvaluateExceptions(opts)
	if err != nil || !reflect.DeepEqual(result.Decisions, []ExceptionDecision{{"EX-2026-001", true}, {"EX-2026-002", true}}) {
		t.Fatal(result, err)
	}
}
