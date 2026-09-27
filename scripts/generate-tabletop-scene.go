//go:build ignore

// Generate the stable read-only home scene from the flagship's typed Go scene.
// Run from the repository root with:
//
//	GOWORK=off go run scripts/generate-tabletop-scene.go
package main

import (
	"encoding/json"
	"fmt"
	"os"

	tabletop "m31labs.dev/gosx/examples/gosx-docs/app/demos/tabletop"
)

func main() {
	data, err := json.MarshalIndent(tabletop.HomeScene(), "", "  ")
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile("examples/gosx-docs/app/demos/tabletop/home-scene.json", append(data, '\n'), 0644); err != nil {
		fail(err)
	}
	fmt.Printf("wrote %d bytes to examples/gosx-docs/app/demos/tabletop/home-scene.json\n", len(data)+1)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
