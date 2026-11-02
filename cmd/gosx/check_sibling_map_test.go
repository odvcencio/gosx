package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCheckResolvesSiblingMapTypes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	source := "package app\n//gosx:island\nfunc Sample(props Props) Node {\nreturn <div>{props.M[\"missing\"]}</div>\n}\n"
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(dir, "types.go")
	if err := os.WriteFile(sibling, []byte("package app\ntype bool string\ntype Props struct { M map[string]bool }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runCheck(path, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("package app\ntype broken {\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runCheck(path, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "lower island Sample") {
		t.Fatalf("broken sibling error = %v", err)
	}
}
