//go:build js && wasm

package browser

import (
	"reflect"
	"syscall/js"
	"testing"
)

func TestLocationAndHistoryPreserveAllURLFields(t *testing.T) {
	g := js.Global()
	location := g.Get("Object").New()
	fields := map[string]string{"href": "https://example.test:444/game?room=ABC123#hero", "origin": "https://example.test:444", "protocol": "https:", "host": "example.test:444", "pathname": "/game", "search": "?room=ABC123", "hash": "#hero"}
	for key, value := range fields {
		location.Set(key, value)
	}
	replaceBrowserGlobal(t, "location", location)
	want := Location{Href: fields["href"], Origin: fields["origin"], Protocol: fields["protocol"], Host: fields["host"], Path: fields["pathname"], Search: fields["search"], Hash: fields["hash"]}
	if got := CurrentLocation(); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	assigned := ""
	reloads := 0
	browserMethod(t, location, "assign", func(_ js.Value, args []js.Value) any { assigned = args[0].String(); return nil })
	browserMethod(t, location, "reload", func(js.Value, []js.Value) any { reloads++; return nil })
	history := g.Get("Object").New()
	replaced := ""
	browserMethod(t, history, "replaceState", func(_ js.Value, args []js.Value) any {
		if len(args) != 3 || !args[0].IsNull() || args[1].String() != "" {
			t.Error("history metadata changed")
		}
		replaced = args[2].String()
		return nil
	})
	replaceBrowserGlobal(t, "history", history)
	Navigate("/")
	ReplaceHistory("?room=ABC123&spectate=1")
	Reload()
	if assigned != "/" || replaced != "?room=ABC123&spectate=1" || reloads != 1 {
		t.Fatal("navigation contract changed")
	}
}

func TestJSONCaptureDisabledSkipsParseAndTracksSequence(t *testing.T) {
	g := js.Global()
	capture := JSONCapture{GlobalName: "testCapture", EnabledFlag: "enabled", PayloadField: "payload", LabelField: "owner", SequenceField: "sequence"}
	host := g.Get("Object").New()
	host.Set("enabled", false)
	replaceBrowserGlobal(t, "testCapture", host)
	json := g.Get("Object").New()
	parses := 0
	browserMethod(t, json, "parse", func(_ js.Value, args []js.Value) any { parses++; return map[string]any{"frame": 42} })
	replaceBrowserGlobal(t, "JSON", json)
	if capture.Enabled() {
		t.Fatal("disabled capture enabled")
	}
	if err := capture.Publish([]byte("invalid JSON"), "seat"); err != nil || parses != 0 {
		t.Fatal("disabled capture paid parse cost", err)
	}
	host.Set("enabled", true)
	if !capture.Enabled() {
		t.Fatal("installed observer not enabled")
	}
	for i := 1; i <= 2; i++ {
		if err := capture.Publish([]byte(`{"frame":42}`), "seat"); err != nil {
			t.Fatal(err)
		}
		if host.Get("sequence").Int() != i {
			t.Fatal("capture sequence", i)
		}
	}
	if parses != 2 || host.Get("payload").Get("frame").Int() != 42 || host.Get("owner").String() != "seat" {
		t.Fatal("capture payload/label contract")
	}
	host.Set("enabled", "true")
	if capture.Enabled() {
		t.Fatal("nonboolean flag enabled capture")
	}
}
