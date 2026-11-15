// Command guidecopy copies the Markdown guides the docs site renders from
// docs/ into app/docs/guides/content/, where the site embeds them. docs/ stays
// the only place a guide is written; the copies are generated and a test fails
// when one differs from its source.
//
//	go generate ./examples/gosx-docs/app/docs
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
)

func main() {
	src := flag.String("src", "", "docs directory")
	dst := flag.String("dst", "", "destination directory")
	flag.Parse()
	if *src == "" || *dst == "" {
		fmt.Fprintln(os.Stderr, "guidecopy: -src and -dst are required")
		os.Exit(2)
	}
	if err := os.MkdirAll(*dst, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "guidecopy:", err)
		os.Exit(1)
	}
	want := map[string]bool{}
	for _, name := range docsapp.GuideSources() {
		want[name] = true
		data, err := os.ReadFile(filepath.Join(*src, name))
		if err != nil {
			fmt.Fprintln(os.Stderr, "guidecopy:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(filepath.Join(*dst, name), data, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "guidecopy:", err)
			os.Exit(1)
		}
	}
	entries, _ := os.ReadDir(*dst)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") && !want[e.Name()] {
			_ = os.Remove(filepath.Join(*dst, e.Name()))
		}
	}
}
