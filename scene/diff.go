package scene

import (
	"bytes"
	"encoding/json"
	"sort"
)

// CommandKind mirrors the Scene3D client command protocol. The numeric values
// are intentionally stable because the browser runtime consumes them directly.
type CommandKind int

const (
	CommandCreateObject CommandKind = iota
	CommandRemoveObject
	CommandSetTransform
	CommandSetMaterial
	CommandSetLight
	CommandSetCamera
	CommandSetParticles
)

// Command is a server-authored Scene3D mutation. Send a JSON array of Commands
// to a mounted Scene3D surface's applyCommands bridge to update the scene
// without replacing the whole page or rehydrating an island.
type Command struct {
	Kind     CommandKind `json:"kind"`
	ObjectID string      `json:"objectId,omitempty"`
	Data     any         `json:"data,omitempty"`
}

// CommandPayload is the create payload shape accepted by the Scene3D runtime.
type CommandPayload struct {
	Kind     string `json:"kind,omitempty"`
	Geometry string `json:"geometry,omitempty"`
	Props    any    `json:"props,omitempty"`
}

// DiffCommands builds a conservative command list that turns previous into
// next for records the current client command bridge can mutate: objects,
// labels, sprites, and lights. Changed records are replaced with remove+create
// instead of partial patches so zero-value resets and omitted JSON fields remain
// correct.
func DiffCommands(previous, next SceneIR) []Command {
	var commands []Command
	diffSceneRecords(&commands, previous.Objects, next.Objects, func(record ObjectIR) string {
		return record.ID
	}, func(record ObjectIR) Command {
		return CreateObjectCommand(record)
	})
	diffSceneRecords(&commands, previous.Labels, next.Labels, func(record LabelIR) string {
		return record.ID
	}, func(record LabelIR) Command {
		return CreateLabelCommand(record)
	})
	diffSceneRecords(&commands, previous.Sprites, next.Sprites, func(record SpriteIR) string {
		return record.ID
	}, func(record SpriteIR) Command {
		return CreateSpriteCommand(record)
	})
	diffSceneRecords(&commands, previous.Lights, next.Lights, func(record LightIR) string {
		return record.ID
	}, func(record LightIR) Command {
		return CreateLightCommand(record)
	})
	return commands
}

// DiffPropsCommands lowers two typed Scene3D props values and diffs the
// resulting SceneIR payloads.
func DiffPropsCommands(previous, next Props) []Command {
	return DiffCommands(previous.SceneIR(), next.SceneIR())
}

// CreateObjectCommand builds a create command for mesh-like Scene3D objects.
func CreateObjectCommand(record ObjectIR) Command {
	return Command{
		Kind:     CommandCreateObject,
		ObjectID: record.ID,
		Data: CommandPayload{
			Geometry: record.Kind,
			Props:    record,
		},
	}
}

// CreateLabelCommand builds a create command for a projected Scene3D label.
func CreateLabelCommand(record LabelIR) Command {
	return Command{
		Kind:     CommandCreateObject,
		ObjectID: record.ID,
		Data: CommandPayload{
			Kind:  "label",
			Props: record,
		},
	}
}

// CreateSpriteCommand builds a create command for a projected Scene3D sprite.
func CreateSpriteCommand(record SpriteIR) Command {
	return Command{
		Kind:     CommandCreateObject,
		ObjectID: record.ID,
		Data: CommandPayload{
			Kind:  "sprite",
			Props: record,
		},
	}
}

// CreateLightCommand builds a create command for a Scene3D light.
func CreateLightCommand(record LightIR) Command {
	return Command{
		Kind:     CommandCreateObject,
		ObjectID: record.ID,
		Data: CommandPayload{
			Kind:  "light",
			Props: record,
		},
	}
}

// RemoveObjectCommand removes any renderable record with the given ID from the
// client runtime maps.
func RemoveObjectCommand(id string) Command {
	return Command{Kind: CommandRemoveObject, ObjectID: id}
}

// MarshalCommands returns the compact JSON payload a hub/server route should
// send to the browser.
func MarshalCommands(commands []Command) ([]byte, error) {
	if commands == nil {
		commands = []Command{}
	}
	return json.Marshal(commands)
}

func diffSceneRecords[T any](commands *[]Command, previous, next []T, id func(T) string, create func(T) Command) {
	prevByID := make(map[string]T, len(previous))
	nextByID := make(map[string]T, len(next))
	for _, record := range previous {
		recordID := id(record)
		if recordID != "" {
			prevByID[recordID] = record
		}
	}
	for _, record := range next {
		recordID := id(record)
		if recordID != "" {
			nextByID[recordID] = record
		}
	}

	var removed []string
	for recordID := range prevByID {
		if _, ok := nextByID[recordID]; !ok {
			removed = append(removed, recordID)
		}
	}
	sort.Strings(removed)
	for _, recordID := range removed {
		*commands = append(*commands, RemoveObjectCommand(recordID))
	}

	for _, record := range next {
		recordID := id(record)
		if recordID == "" {
			continue
		}
		previousRecord, existed := prevByID[recordID]
		if existed && sceneRecordJSONEqual(previousRecord, record) {
			continue
		}
		if existed {
			*commands = append(*commands, RemoveObjectCommand(recordID))
		}
		*commands = append(*commands, create(record))
	}
}

func sceneRecordJSONEqual[T any](a, b T) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(aj, bj)
}
