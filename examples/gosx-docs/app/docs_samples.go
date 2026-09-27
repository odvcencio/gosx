package docs

import docsamples "m31labs.dev/gosx/examples/gosx-docs/samples"

// DocSample returns the embedded source rendered by a documentation page.
// The sample tree is also compiled or checked by the documentation tests.
func DocSample(path string) string {
	return docsamples.MustRead(path)
}
