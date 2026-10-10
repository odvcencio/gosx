package budget

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"unicode/utf8"
)

// TrustedApproval is native evidence supplied by the trusted review adapter.
// It is never decoded from budget JSON or from command-line approval flags.
type TrustedApproval struct {
	ReviewID        int64
	Role            string
	ExceptionSHA256 string
}

// ExceptionSHA256 binds approval to the complete canonical exception content.
// The review ID is omitted so recording an existing review cannot change the
// reviewed content. The adapter must verify APPROVED state, account role and
// the budget blob at the reviewed commit before constructing TrustedApproval.
func ExceptionSHA256(e Exception) (string, error) {
	if err := validateExceptionShape(e); err != nil {
		return "", err
	}
	data, _ := json.Marshal(e)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return "", inputReference(invalidInput(""), "exception", "")
	}
	delete(fields, "approvalReview")
	data, _ = json.Marshal(fields) // encoding/json sorts map keys.
	sum := sha256.Sum256(append(data, '\n'))
	return hex.EncodeToString(sum[:]), nil
}

func validateExceptionShape(e Exception) error {
	for _, field := range []struct{ name, value string }{{"id", e.ID}, {"scope", e.Scope}, {"metric", e.Metric}, {"policy", e.Policy}, {"reasonCode", e.ReasonCode}, {"ownerRole", e.OwnerRole}, {"approvedRole", e.ApprovedRole}, {"expires", e.Expires}} {
		if !utf8.ValidString(field.value) {
			return &InputError{Code: "invalid-input", Reference: "exception", Pointer: "/" + field.name}
		}
	}
	data, err := json.Marshal(e)
	if err != nil {
		return inputReference(invalidInput(""), "exception", "")
	}
	var checked Exception
	if err := decodeInput(data, "Exception", &checked); err != nil {
		input := err.(*InputError)
		return &InputError{Code: input.Code, Reference: "exception", Pointer: input.Pointer}
	}
	return nil
}

func approvalIndex(proofs []TrustedApproval) (map[TrustedApproval]bool, error) {
	if len(proofs) > 64 {
		return nil, inputReference(invalidInput(""), "approvals", "")
	}
	seen := map[TrustedApproval]bool{}
	roles := map[int64]string{}
	for i, proof := range proofs {
		pointer := pointerChild("", strconv.Itoa(i))
		if proof.ReviewID <= 0 || proof.Role != "reviewer" && proof.Role != "owner" || validateInput(proof.ExceptionSHA256, inputDefinitions["SHA"]) != nil || seen[proof] || roles[proof.ReviewID] != "" && roles[proof.ReviewID] != proof.Role {
			return nil, inputReference(invalidInput(pointer), "approvals", "")
		}
		seen[proof], roles[proof.ReviewID] = true, proof.Role
	}
	return seen, nil
}
