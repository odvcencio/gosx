package main

import (
	"fmt"
	"time"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/aot"
)

func validateIslandsBackend(backend string) error {
	if backend != "" && backend != "vm" && backend != "auto" {
		return fmt.Errorf("build.islands.backend must be vm or auto")
	}
	return nil
}

// Auto currently proves scalar contracts while emitting the complete VM
// artifact. Compiled runtime selection remains disabled.
func admitBuildIslands(programs []*IslandProgramSource, backend string, check func(*ir.Program, int) (aot.Unit, error)) error {
	if err := validateIslandsBackend(backend); err != nil {
		return err
	}
	if backend != "auto" {
		return nil
	}
	start := time.Now()
	admitted := 0
	for _, candidate := range programs {
		unit, err := check(candidate.Candidate, candidate.ComponentIndex)
		if err != nil {
			fmt.Printf("  Island %s: VM (%v)\n", candidate.Name, err)
			continue
		}
		candidate.Contract = &unit.Contract
		admitted++
	}
	fmt.Printf("  Island admission: %d/%d scalar contracts in %s\n", admitted, len(programs), time.Since(start))
	return nil
}
