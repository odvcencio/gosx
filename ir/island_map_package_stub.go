//go:build tinygo || js

package ir

import (
	"fmt"

	"m31labs.dev/gosx/island/program"
)

func islandPackageMapTypes(_ *Program, _ string) (map[string]program.ExprType, error) {
	return nil, fmt.Errorf("package type resolution is unavailable on this target")
}
