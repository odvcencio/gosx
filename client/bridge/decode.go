package bridge

import (
	"fmt"

	"github.com/odvcencio/gosx/island/program"
)

// DecodeProgram dispatches to JSON or binary decoder based on format.
func DecodeProgram(data []byte, format string) (*program.Program, error) {
	switch format {
	case "json":
		return program.DecodeJSON(data)
	case "bin":
		return program.DecodeBinary(data)
	default:
		return nil, fmt.Errorf("unknown program format: %q", format)
	}
}
