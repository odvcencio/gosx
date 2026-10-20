//go:build !(js && wasm)

package browser

import "errors"

type RequestGuard struct{}

func GuardRequests(RequestPolicy) (*RequestGuard, error) {
	return nil, errors.New("browser: request guard runtime unavailable")
}
func (*RequestGuard) Dispose() {}
