//go:build !linux

package budgetci

import (
	"errors"
	"os/exec"
)

// The hosted production wrapper requires Linux process ownership. Hardware
// lab execution has a separate platform lock and runner.
func productionSupported() bool     { return false }
func prepareProduction(*exec.Cmd)   {}
func stopProduction(int) error      { return errors.New("unsupported production platform") }
func runProduction(*exec.Cmd) error { return errors.New("unsupported production platform") }
