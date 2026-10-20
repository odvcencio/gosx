package budget

import "io"

// ReviewExceptionDigests validates a reviewed budget blob without resolving its
// historical references. Only complete exception content is used for approval;
// current allocation and expiry rules still apply in EvaluateExceptions.
func ReviewExceptionDigests(r io.Reader) (map[string]string, error) {
	if r == nil {
		return nil, inputReference(invalidInput(""), "review", "")
	}
	data, err := io.ReadAll(io.LimitReader(r, maxInputBytes+1))
	if err != nil || len(data) > maxInputBytes {
		return nil, inputReference(invalidInput(""), "review", "")
	}
	var file File
	if err := decodeInput(data, "Budget", &file); err != nil {
		typed := err.(*InputError)
		return nil, &InputError{Code: typed.Code, Reference: "review", Pointer: typed.Pointer}
	}
	digests := map[string]string{}
	for _, exception := range file.Exceptions {
		if _, exists := digests[exception.ID]; exists {
			return nil, inputReference(invalidInput("/exceptions"), "review", "")
		}
		digest, err := ExceptionSHA256(exception)
		if err != nil {
			return nil, &InputError{Code: "invalid-input", Reference: "review", Pointer: "/exceptions"}
		}
		digests[exception.ID] = digest
	}
	return digests, nil
}
