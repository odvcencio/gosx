package main

import (
	"fmt"
	"os"
	"time"

	"github.com/odvcencio/gosx/perf"
)

func cmdRepl() {
	url := argOrDefault(2, "")
	if url == "--dev" || url == "" {
		url = "http://localhost:3000"
	}

	d, err := perf.New(perf.WithHeadless(false), perf.WithTimeout(5*time.Minute))
	if err != nil {
		fatal("gosx repl: %v", err)
	}
	defer d.Close()

	if err := perf.InjectDriver(d); err != nil {
		fatal("gosx repl: inject: %v", err)
	}

	if err := d.Navigate(url); err != nil {
		fatal("gosx repl: navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		fmt.Fprintf(os.Stderr, "gosx repl: warning: wait ready: %v\n", err)
	}

	if err := perf.RunREPL(d, url); err != nil {
		fatal("gosx repl: %v", err)
	}
}
