//go:build !(js && wasm)

package client

// newBrowserLink reports that there is no browser here.
func newBrowserLink() (rtcLink, error) { return nil, errNoBrowser }
