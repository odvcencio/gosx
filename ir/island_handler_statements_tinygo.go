//go:build tinygo || js

package ir

import "m31labs.dev/gosx/island/program"

// Go statements are compiled on the host; browser runtimes consume bytecode.
func (l *islandLowerer) lowerHandlerStatements(_ []string, _ *ExprScope) ([]program.ExprID, bool, error) {
	return nil, false, nil
}
