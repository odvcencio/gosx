package hydrate

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx/command"
)

func TestManifestAddCommandsValidatesAndMarshals(t *testing.T) {
	m := NewManifest()
	cmd := command.Command{ID: "edit.undo", Title: "Undo", Keys: []string{"Mod+Z"}, Action: command.Action{Signal: "$edit", Value: "undo"}}
	if err := m.AddCommands(cmd); err != nil {
		t.Fatal(err)
	}
	if err := m.AddCommands(command.Command{ID: "help", Title: "Help", Reserved: true}, cmd); err == nil {
		t.Fatal("duplicate command accepted")
	}
	if len(m.Commands) != 1 {
		t.Fatal("failed registration changed commands")
	}
	data, err := json.Marshal(m.WithBasePath("/app"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"commands":[{"id":"edit.undo","title":"Undo","keys":["Mod+Z"],"action":{"signal":"$edit","value":"undo"}}]`) {
		t.Fatalf("manifest lacks commands: %s", data)
	}
	if empty, _ := json.Marshal(NewManifest()); strings.Contains(string(empty), `"commands"`) {
		t.Fatalf("empty manifest includes commands: %s", empty)
	}
}
