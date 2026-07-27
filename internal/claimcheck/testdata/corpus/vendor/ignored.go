package ignored

// The walk never enters a directory named vendor, so this false claim must not
// reach the checker. The meta test asserts that no result names this file.
//
//	gosx:claim has target.go `func Gamma` case-skipped-vendor
func ignoredClaim() {}
