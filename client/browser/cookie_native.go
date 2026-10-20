//go:build !(js && wasm)

package browser

import "errors"

func Cookie() string         { return "" }
func SetCookie(string) error { return errors.New("browser: document unavailable") }
