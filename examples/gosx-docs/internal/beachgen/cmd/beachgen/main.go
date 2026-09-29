// Command beachgen regenerates the Blackglass Beach assets: terrain, sea
// stacks, monolith and bathymetry (models), and the baked sky IBL for every
// period (env files plus the descriptors the scene program embeds).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
)

func main() {
	out := flag.String("out", "examples/gosx-docs/public/models/blackglass", "model output directory")
	env := flag.String("env", "examples/gosx-docs/public/env/blackglass-beach", "baked IBL output directory")
	descriptors := flag.String("descriptors", "examples/gosx-docs/app/demos/beacon/ibl-beach", "IBL descriptor directory")
	flag.Parse()
	if err := run(*out, *env, *descriptors); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out, env, descriptors string) error {
	if err := beachgen.Write(out, 0xB1AC6A55); err != nil {
		return err
	}
	for _, dir := range []string{env, descriptors} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	for _, period := range beachgen.Periods {
		files, descriptor, err := beachgen.BakeSkyIBL(period, "/env/blackglass-beach/"+period)
		if err != nil {
			return fmt.Errorf("%s: %w", period, err)
		}
		for suffix, data := range files {
			if err := os.WriteFile(filepath.Join(env, period+"."+suffix), data, 0o644); err != nil {
				return err
			}
		}
		encoded, err := json.MarshalIndent(descriptor, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(descriptors, period+".json"), append(encoded, '\n'), 0o644); err != nil {
			return err
		}
	}
	return nil
}
