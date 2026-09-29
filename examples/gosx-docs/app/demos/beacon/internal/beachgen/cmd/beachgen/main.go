package main

import (
	"flag"
	"fmt"
	"os"

	"m31labs.dev/gosx/examples/gosx-docs/app/demos/beacon/internal/beachgen"
)

func main() {
	out := flag.String("out", "examples/gosx-docs/public/models/blackglass", "output directory")
	flag.Parse()
	if err := beachgen.Write(*out, 0xB1AC6A55); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
