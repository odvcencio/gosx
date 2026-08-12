package format

import (
	"bytes"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestSourceFormatsStrictComponentIdempotently(t *testing.T) {
	source := []byte(`package app
type CardProps struct {
	Title string
	Body string
}

component Card(props: CardProps) {
	return <article><h2>{props.Title}</h2><p>{props.Body}</p></article>
}
`)
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if !strings.Contains(string(formatted), "component Card(props: CardProps)") {
		t.Fatalf("strict declaration was not preserved:\n%s", formatted)
	}
	if _, err := gosx.Compile(formatted); err != nil {
		t.Fatalf("formatted strict source does not compile: %v\n%s", err, formatted)
	}
	again, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source second pass: %v", err)
	}
	if !bytes.Equal(formatted, again) {
		t.Fatalf("format is not idempotent\nfirst:\n%s\nsecond:\n%s", formatted, again)
	}
}
