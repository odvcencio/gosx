// Package telemetryauthority grants framework-only registry registration.
package telemetryauthority

// Key is opaque. Its zero value and independently constructed values confer
// no authority. This internal package cannot be imported by applications.
type Key struct{ proof *token }

type token struct{ marker byte }

var authority = &token{marker: 1}

func New() Key            { return Key{proof: authority} }
func (k Key) Valid() bool { return k.proof == authority }
