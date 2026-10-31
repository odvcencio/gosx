//go:build js && wasm

package main

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/client/bridge"
)

// sendDirect writes text into the reserved buffer and applies it, as the page does.
func sendDirect(t *testing.T, d *directBatch, text string, changedOnly bool) error {
	t.Helper()

	buf := d.reserve(len(text) * 3)
	if len(buf) < len(text)*3 {
		t.Fatalf("reserve(%d) gave %d bytes", len(text)*3, len(buf))
	}
	return d.apply(copy(buf, text), changedOnly)
}

func TestDirectBatchAppliesWhatThePageWrote(t *testing.T) {
	b := bridge.New()
	d := &directBatch{bridge: b}
	var told []string
	b.SetSharedSignalBatchCallback(func(updates []bridge.SignalUpdate) {
		for _, update := range updates {
			told = append(told, update.Name+"="+update.JSON)
		}
	})

	if err := sendDirect(t, d, `{ "$hud" : {"tick": 4, "name": "h\u00e9llo \u2603"} , "$n": 1 }`, false); err != nil {
		t.Fatal(err)
	}
	if len(told) != 2 {
		t.Fatalf("told %v", told)
	}
	if hud, err := b.GetSharedSignalJSON("$hud"); err != nil || hud != `{"name":"héllo ☃","tick":4}` {
		t.Fatalf("$hud = %q (%v)", hud, err)
	}

	// A changed-only batch tells nothing about what did not change.
	told = nil
	if err := sendDirect(t, d, `{"$n": 1, "$m": 2}`, true); err != nil {
		t.Fatal(err)
	}
	if len(told) != 1 || told[0] != "$m=2" {
		t.Fatalf("changed-only told %v", told)
	}
}

func TestDirectBatchRefusesWhatTheFunctionRefuses(t *testing.T) {
	d := &directBatch{bridge: bridge.New()}
	for _, text := range []string{`{"$a":`, `[1]`, `{" ":1}`} {
		err := sendDirect(t, d, text, false)
		if err == nil || strings.HasPrefix(err.Error(), "error:") {
			t.Errorf("%q: err = %v (the page adds the \"error: \" prefix)", text, err)
		}
	}
	// The function reports the same refusals with the prefix; the texts must agree.
	for _, text := range []string{`{"$a":`, `[1]`, `{" ":1}`} {
		direct := sendDirect(t, d, text, false)
		viaFunction := bridge.New().SetSharedSignalBatchJSON(text)
		if direct == nil || viaFunction == nil || direct.Error() != viaFunction.Error() {
			t.Errorf("%q: direct %v, function %v", text, direct, viaFunction)
		}
	}
	if err := (&directBatch{}).apply(0, false); err == nil {
		t.Error("an unregistered entry applied a batch")
	}
	if err := d.apply(-1, false); err == nil {
		t.Error("a negative length was accepted")
	}
	if err := d.apply(len(d.reserve(10))+1, false); err == nil {
		t.Error("a length past the buffer was accepted")
	}
}

func TestDirectBatchReserveGrowsByDoublingAndKeepsWhatItNeeds(t *testing.T) {
	d := &directBatch{}
	small := d.reserve(10)
	if len(small) != minStagingBytes {
		t.Fatalf("reserve(10) = %d bytes, want %d", len(small), minStagingBytes)
	}
	big := d.reserve(minStagingBytes + 1)
	if len(big) != 2*minStagingBytes {
		t.Fatalf("reserve(%d) = %d bytes", minStagingBytes+1, len(big))
	}
	again := d.reserve(10)
	if len(again) != 2*minStagingBytes || &again[0] != &big[0] {
		t.Fatal("a smaller request must keep the buffer it has")
	}
}

func TestDirectBatchDropsAnOutsizedBufferOnceItsTextIsCopied(t *testing.T) {
	d := &directBatch{bridge: bridge.New()}
	buf := d.reserve(maxStagingBytes + 1)
	n := copy(buf, `{"$big":1}`)
	if err := d.apply(n, false); err != nil {
		t.Fatal(err)
	}
	if d.staging != nil {
		t.Fatalf("kept %d bytes between batches", cap(d.staging))
	}
}

func TestDirectBatchSentFromASubscriberDoesNotDisturbTheOneBeingApplied(t *testing.T) {
	// The batch wakes a subscriber that sends a batch of its own through the same buffer.
	// The outer batch has been copied out of the buffer by then, so nothing it holds changes.
	b := bridge.New()
	d := &directBatch{bridge: b}
	var inner error
	sent := false
	b.SetSharedSignalBatchCallback(func(updates []bridge.SignalUpdate) {
		if sent {
			return
		}
		sent = true
		inner = sendDirect(t, d, `{"$inner":"ZZZZZZZZZZZZZZZZZZZZ"}`, false)
	})
	if err := sendDirect(t, d, `{"$outer":"aaaaaaaaaaaaaaaaaaaa","$other":{"k":[1,2,3]}}`, false); err != nil {
		t.Fatal(err)
	}
	if inner != nil {
		t.Fatalf("inner batch: %v", inner)
	}
	for name, want := range map[string]string{"$outer": `"aaaaaaaaaaaaaaaaaaaa"`, "$other": `{"k":[1,2,3]}`, "$inner": `"ZZZZZZZZZZZZZZZZZZZZ"`} {
		if got, err := b.GetSharedSignalJSON(name); err != nil || got != want {
			t.Errorf("%s = %q (%v), want %s", name, got, err, want)
		}
	}
}

func TestRegisterRuntimePointsTheDirectEntryAtItsBridge(t *testing.T) {
	b := bridge.New()
	previous := direct.bridge
	t.Cleanup(func() { direct.bridge = previous })
	registerRuntime(b)
	if direct.bridge != b {
		t.Fatal("the direct batch entry serves another bridge than the runtime")
	}
}
