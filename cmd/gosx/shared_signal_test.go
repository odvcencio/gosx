package main

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestCheckSharedSignalConstructors(t *testing.T) {
	dir := newInvalidStrictStarter(t, "shared-signal-check")
	path := filepath.Join(dir, "app", "page.gsx")
	mustWriteFile(t, path, `package app
import "m31labs.dev/gosx/signal"

//gosx:island
component Page() {
    selected := signal.NewShared[int]("selection", 3)
    revision := signal.Shared("$revision", 1)
    return <button onClick={func() { selected.Set(revision.Get()) }}>{selected.Get()}</button>
}
`)
	if err := runCheck(path, &bytes.Buffer{}); err != nil {
		t.Fatalf("gosx check rejected shared constructors: %v", err)
	}
	if err := runCheckTypes(path, &bytes.Buffer{}); err != nil {
		t.Fatalf("Go type check rejected shared constructors: %v", err)
	}
}
