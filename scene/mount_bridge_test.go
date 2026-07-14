package scene

import (
	"encoding/json"
	"testing"
)

func TestMountCommandBatchMarshal(t *testing.T) {
	batch := MountCommandBatch{Revision: 7, Commands: []Command{{Kind: CommandRemoveObject, ObjectID: "piece-4"}}}
	data, err := batch.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var got MountCommandBatch
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Revision != 7 || len(got.Commands) != 1 || got.Commands[0].ObjectID != "piece-4" {
		t.Fatalf("unexpected round trip: %#v", got)
	}
}

func TestMountCommandBatchRejectsZeroRevision(t *testing.T) {
	if _, err := (MountCommandBatch{}).Marshal(); err == nil {
		t.Fatal("expected zero revision to be rejected")
	}
}

func TestMountCommandBatchEncodesEmptyCommandsAsArray(t *testing.T) {
	data, err := (MountCommandBatch{Revision: 1}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"revision":1,"commands":[]}` {
		t.Fatalf("unexpected payload: %s", data)
	}
}
