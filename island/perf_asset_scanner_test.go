package island

import "m31labs.dev/gosx/island/perfscan"

// Package island cannot parse HTML without golang.org/x/net, so its tests
// install the tooling scanner.
func init() {
	DefaultPerfHeadScanner = func(head string) ([]PerfHeadRef, error) {
		refs, err := perfscan.Scan(head)
		out := make([]PerfHeadRef, len(refs))
		for i, ref := range refs {
			out[i] = PerfHeadRef(ref)
		}
		return out, err
	}
}
