package corpus

// Every marker below holds. The meta test expects each one to pass.

// Alpha is the one function the target exports for the happy path.
//
//	gosx:claim has target.go `func Alpha` case-ok-has
func usesAlpha() {}

// The target never grew a Gamma helper.
//
//	gosx:claim lacks target.go `func Gamma` case-ok-lacks
func noGamma() {}

// Two names start with the same prefix.
//
//	gosx:claim count=2 target.go `func Beta` case-ok-count
func twoBetas() {}

// A target in a subdirectory resolves from the corpus root, not from this file.
//
//	gosx:claim has sub/nested.go `func Nested` case-ok-subdir
func usesNested() {}
