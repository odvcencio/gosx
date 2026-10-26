package hydrate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHubDocumentRegistersOnlyItsOptInFeature(t *testing.T) {
	m := NewManifest()
	id, err := m.AddHubDocument("notes", "/notes/ws", HubDocumentConfig{RootID: "notes", Version: 7})
	if err != nil {
		t.Fatal(err)
	}
	if id != m.Hubs[0].ID || strings.Join(m.Features, ",") != "hub-document" {
		t.Fatalf("manifest: %+v", m)
	}
	c := m.Hubs[0].Document
	if c.Version != 7 || c.DebounceMS != 100 || c.CursorThrottleMS != 60 || c.CursorStaleMS != 15000 || c.MaxTextBytes != 64*1024 {
		t.Fatalf("config: %+v", c)
	}
	if c.Events.Edit != "doc:edit" || c.Events.Sync != "doc:sync" || c.Events.Update != "doc:update" {
		t.Fatalf("events: %+v", c.Events)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var got Manifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if *got.Hubs[0].Document != *c {
		t.Fatalf("document config lost in JSON: %s", data)
	}
	plain := NewManifest()
	plain.AddHub("other", "/other", nil)
	data, _ = json.Marshal(plain)
	if strings.Contains(string(data), `"document"`) || len(plain.Features) != 0 {
		t.Fatalf("plain hub acquired document work: %s", data)
	}
}

func TestHubDocumentRejectsInvalidConfigWithoutPartialRegistration(t *testing.T) {
	for name, config := range map[string]HubDocumentConfig{
		"missing root": {}, "whitespace root": {RootID: "bad id"}, "control root": {RootID: "bad\x00id"},
		"negative delay": {RootID: "notes", DebounceMS: -1}, "busy cursor": {RootID: "notes", CursorThrottleMS: 1},
		"oversize": {RootID: "notes", MaxTextBytes: 1024*1024 + 1}, "inexact version": {RootID: "notes", Version: 1 << 53},
		"reserved event":  {RootID: "notes", Events: HubDocumentEvents{Update: "__welcome"}},
		"duplicate event": {RootID: "notes", Events: HubDocumentEvents{Edit: "doc:update"}},
	} {
		t.Run(name, func(t *testing.T) {
			m := NewManifest()
			if _, err := m.AddHubDocument("notes", "/ws", config); err == nil {
				t.Fatal("invalid config accepted")
			}
			if len(m.Features)+len(m.Hubs) != 0 {
				t.Fatal("invalid binding mutated the manifest")
			}
		})
	}
	m := NewManifest()
	if _, err := m.AddHubDocument("notes", "/ws", HubDocumentConfig{RootID: "notes"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddHubDocument("second", "/second", HubDocumentConfig{RootID: "notes"}); err == nil {
		t.Fatal("two bindings own the same root")
	}
	if len(m.Hubs) != 1 {
		t.Fatal("duplicate root was registered")
	}
}
