package main

import (
	"fmt"
	"os"

	"github.com/odvcencio/gosx/perf"
)

func cmdPerf() {
	// For now, just validate we can connect to a URL.
	// Full flag parsing comes in a later task.
	url := argOrDefault(2, "")
	if url == "" {
		fatal("usage: gosx perf <url>")
	}

	d, err := perf.New()
	if err != nil {
		fatal("gosx perf: %v", err)
	}
	defer d.Close()

	if err := d.Navigate(url); err != nil {
		fatal("gosx perf: navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		fatal("gosx perf: wait: %v", err)
	}

	fmt.Fprintf(os.Stderr, "gosx perf: connected to %s\n", url)
}
