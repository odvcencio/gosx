package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const budgetJSON = `{"schema":"gosx.wire-budget/v1","tolerance":{"bytesPercent":2,"bytesMin":512},"apps":{"a":{"/":{"limits":{"totalWireBytes":100},"require":["no-cookie"]}}}}`

func TestRatchetNeverPassesOnAMissingBase(t *testing.T) {
	dir := t.TempDir()
	head := filepath.Join(dir, "head.json")
	if err := os.WriteFile(head, []byte(budgetJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	for name, args := range map[string][]string{
		"missing file": {"ratchet", "-base", filepath.Join(dir, "absent.json"), "-head", head},
		"empty file":   {"ratchet", "-base", empty, "-head", head},
		"no base flag": {"ratchet", "-head", head},
	} {
		if err := run(args, &out, &out); err == nil {
			t.Errorf("%s: ratchet passed, want an error", name)
		}
	}
	if err := run([]string{"ratchet", "-initial", "-head", head}, &out, &out); err != nil {
		t.Fatalf("-initial: %v", err)
	}
	if err := run([]string{"ratchet", "-base", head, "-head", head}, &out, &out); err != nil {
		t.Fatalf("identical budgets: %v", err)
	}

	raised := filepath.Join(dir, "raised.json")
	if err := os.WriteFile(raised, bytes.Replace([]byte(budgetJSON), []byte(`"totalWireBytes":100`), []byte(`"totalWireBytes":101`), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"ratchet", "-base", head, "-head", raised}, &out, &out); !errors.Is(err, errGate) {
		t.Fatalf("raised limit: err = %v, want the gate to fail", err)
	}
}
