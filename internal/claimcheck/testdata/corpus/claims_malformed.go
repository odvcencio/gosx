package corpus

// Every marker below carries the token and does not parse. The meta test
// expects each one to be reported as malformed, with a reason that names the
// mistake. None of them may read as a passing claim.

// gosx:claim case-bad-no-fields
func noFields() {}

// gosx:claim contains target.go `func Alpha` case-bad-unknown-verb
func unknownVerb() {}

// gosx:claim count target.go `func Beta` case-bad-count-without-number
func countWithoutNumber() {}

// gosx:claim has=2 target.go `func Alpha` case-bad-has-with-count
func hasWithCount() {}

// gosx:claim has target.go `func Alpha case-bad-unclosed-backtick
func unclosedBacktick() {}

// gosx:claim has target.go `func Alpha(` case-bad-uncompilable-regexp
func uncompilableRegexp() {}

// gosx:claim has /etc/passwd `root` case-bad-absolute-target
func absoluteTarget() {}

// gosx:claim has ./target.go `func Alpha` case-bad-dot-relative-target
func dotRelativeTarget() {}

// gosx:claim has claims_malformed.go `func selfTarget` case-bad-self-target
func selfTarget() {}
