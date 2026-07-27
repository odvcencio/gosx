package corpus

// Every marker below parses and is false. The meta test expects each one to be
// reported as a false claim, not as a malformed marker.

// This comment names a helper the target never had.
//
//	gosx:claim has target.go `func Gamma` case-false-has
func missingSymbol() {}

// This is the direction that costs most: the comment denies a feature that
// works, so the next author deletes working code.
//
//	gosx:claim lacks target.go `func Alpha` case-false-lacks
func deniesWorkingCode() {}

// The target grew a second name after this count was written.
//
//	gosx:claim count=1 target.go `func Beta` case-false-count
func staleCount() {}

// The target file moved and nothing updated the claim.
//
//	gosx:claim has gone_away.go `func Anything` case-false-missing-target
func targetDeleted() {}
