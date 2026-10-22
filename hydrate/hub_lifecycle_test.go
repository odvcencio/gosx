package hydrate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSetHubLifecycleAppliesDefaultsAndRoundTrips(t *testing.T) {
	m := NewManifest()
	id := m.AddHub("live", "/ws", nil)
	err := m.SetHubLifecycle(id, HubLifecycle{ConnectionSignal: "$live.connection", Resync: &HubResyncConfig{PeriodMS: 180000, OffsetMS: 2200, InitialDelayMS: 1200}})
	if err != nil {
		t.Fatal(err)
	}
	entry := m.Hubs[0]
	if entry.ConnectionSignal != "$live.connection" || entry.Resync == nil || entry.Resync.Event != "resync" || entry.Resync.MinIntervalMS != 1200 {
		t.Fatalf("defaults not applied: %+v", entry)
	}
	data, _ := json.Marshal(entry)
	for _, want := range []string{`"connectionSignal":"$live.connection"`, `"resync":{`, `"periodMs":180000`, `"offsetMs":2200`, `"initialDelayMs":1200`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %s in %s", want, data)
		}
	}
	plain, _ := json.Marshal(HubEntry{ID: "x", Name: "n", Path: "/p"})
	if strings.Contains(string(plain), "resync") || strings.Contains(string(plain), "connectionSignal") {
		t.Fatalf("zero lifecycle must be omitted: %s", plain)
	}
}

func TestSetHubLifecycleRejectsBadInput(t *testing.T) {
	m := NewManifest()
	id := m.AddHub("live", "/ws", nil)
	cases := map[string]HubLifecycle{
		"signal with space":     {ConnectionSignal: "bad name"},
		"reserved event":        {Resync: &HubResyncConfig{Event: "__resync"}},
		"interval too small":    {Resync: &HubResyncConfig{MinIntervalMS: 10}},
		"unknown trigger":       {Resync: &HubResyncConfig{On: []string{"scroll"}}},
		"offset past period":    {Resync: &HubResyncConfig{PeriodMS: 1000, OffsetMS: 2000}},
		"offset without period": {Resync: &HubResyncConfig{OffsetMS: 10}},
	}
	for name, cfg := range cases {
		if err := m.SetHubLifecycle(id, cfg); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
	if err := m.SetHubLifecycle("gosx-hub-9", HubLifecycle{ConnectionSignal: "$x"}); err == nil {
		t.Fatal("unknown hub id must error")
	}
}

func TestSceneCommandsWhenValidation(t *testing.T) {
	m := NewManifest()
	m.AddHub("scene", "/ws", []HubBinding{
		{Event: "update", SceneMountID: "scene", SceneCommands: true, SceneCommandsWhen: "revealed"},
		{Event: "update", SceneMountID: "scene", SceneCommands: true, SceneCommandsWhen: "painted"},
		{Event: "update", SceneMountID: "scene", SceneCommands: true, SceneCommandsWhenTimeoutMS: 500},
	})
	if got := len(m.Hubs[0].Bindings); got != 1 {
		t.Fatalf("kept %d bindings, want 1 (unknown gate and timeout-without-gate are dropped)", got)
	}
	err := m.Validate()
	if err == nil || !strings.Contains(err.Error(), "sceneCommandsWhen") {
		t.Fatalf("error must name sceneCommandsWhen: %v", err)
	}
	data, _ := json.Marshal(m.Hubs[0].Bindings[0])
	if !strings.Contains(string(data), `"sceneCommandsWhen":"revealed"`) {
		t.Fatalf("round trip: %s", data)
	}
}
