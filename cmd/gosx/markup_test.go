package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestMarkupCommandsExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name, source, position, message string
		compileOK                       bool
	}{
		{"mismatched", "package app\nfunc Page() Node {\n\treturn <h2>Next steps</h3>\n}\n", "page.gsx:3:23:", "expected </h2>", false},
		{"unclosed", "package app\nfunc Page() Node {\n\treturn <h2>Next steps\n}\n", "page.gsx:3:9:", "add </h2>", false},
		{"zero components", "package app\nvar content = <h2>Next steps</h2>\n", "page.gsx:2:15:", "markup produced zero components", true},
		{"valid", "package app\nfunc Page() Node {\n\treturn <h2>Next steps</h2>\n}\n", "", "", true},
		{"no markup", "package app\nvar text = `<h2>`\n", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTempFile(t, dir, "page.gsx", tc.source)
			for _, command := range []string{"check", "compile"} {
				t.Run(command, func(t *testing.T) {
					stdout, stderr, err := runGosxHelpCommand(t, dir, command, "page.gsx")
					wantOK := tc.message == "" || command == "compile" && tc.compileOK
					if wantOK {
						if err != nil {
							t.Fatalf("command failed: %v\n%s", err, stderr)
						}
						return
					}
					if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
						t.Fatalf("error = %v, want exit code 1", err)
					}
					for _, want := range []string{tc.position, tc.message, "hint:"} {
						if !strings.Contains(stderr, want) {
							t.Fatalf("stderr %q missing %q", stderr, want)
						}
					}
					if strings.Contains(stderr, "ok:") || stdout != "" {
						t.Fatalf("failure printed success: stdout=%q stderr=%q", stdout, stderr)
					}
				})
			}
		})
	}
}
