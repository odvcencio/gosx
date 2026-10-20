package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestParseViewport(t *testing.T) {
	width, height, err := parseViewport("1440x900")
	if err != nil {
		t.Fatal(err)
	}
	if width != 1440 || height != 900 {
		t.Fatalf("viewport = %dx%d", width, height)
	}
}

func TestParseViewportRejectsInvalidValue(t *testing.T) {
	if _, _, err := parseViewport("wide"); err == nil {
		t.Fatal("parseViewport accepted invalid value")
	}
}

func TestSplitCSVTrimsEmptyValues(t *testing.T) {
	got := splitCSV(" R00, R01,,R08 ")
	want := []string{"R00", "R01", "R08"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitCSV() = %#v, want %#v", got, want)
	}
}

func TestPerfOuroborosUsageMentionsCanonicalEvidenceFlags(t *testing.T) {
	var buf bytes.Buffer
	perfOuroborosUsage(&buf)
	out := buf.String()
	for _, want := range []string{"--evidence-root", "--pixel-manifest", "pixel-evidence.json"} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage missing %q:\n%s", want, out)
		}
	}
}
