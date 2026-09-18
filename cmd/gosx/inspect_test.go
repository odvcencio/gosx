package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectJSONContract(t *testing.T) {
	for _, tc := range []struct {
		name, source  string
		withIR, valid bool
	}{
		{"summary", "package app\ncomponent Page() {\nreturn <main>Ready</main>\n}\n", false, true},
		{"ir", "package app\ncomponent Page() {\nreturn <main>Ready</main>\n}\n", true, true},
		{"warning", "package app\nfunc Page(props any) Node {\nreturn <main>{props.Name}</main>\n}\n", false, true},
		{"syntax", "package app\ncomponent Page() {\nreturn <main>{</main>\n}\n", false, false},
		{"validation", "package app\nfunc broken() Node {\nreturn <main>Ready</main>\n}\n", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "page.gsx")
			if err := os.WriteFile(file, []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{file}
			if tc.withIR {
				args = append([]string{"--ir"}, args...)
			}
			var out bytes.Buffer
			err := runInspect(args, &out)
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected command error: %v", err)
			}
			var report inspectReport
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("stdout must be JSON on compiler failure too: %v\n%s", err, out.Bytes())
			}
			if report.Valid != tc.valid || report.SchemaVersion != 1 || report.GoSXVersion == "" || report.File != file || len(report.Stages) == 0 {
				t.Fatalf("incomplete inspection report: %+v", report)
			}
			if report.SourceHash != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))) {
				t.Fatal("inspection is not bound to the analyzed source revision")
			}
			if tc.withIR != (report.Program != nil) {
				t.Fatalf("unexpected IR presence: %+v", report)
			}
			if tc.name == "warning" || !tc.valid {
				if len(report.Diagnostics) == 0 || report.Diagnostics[0].Span.File != file {
					t.Fatalf("missing source diagnostics: %+v", report)
				}
			}
		})
	}
}

func TestInspectRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"--unknown"}, {"a.gsx", "b.gsx"}} {
		var out bytes.Buffer
		if err := runInspect(args, &out); err == nil || out.Len() != 0 {
			t.Fatalf("unexpected argument handling for %q: %v, %s", args, err, out.Bytes())
		}
	}
}
