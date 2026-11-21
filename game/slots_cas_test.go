package game

import (
	"errors"
	"testing"

	"m31labs.dev/gosx/game/storage"
)

// lockedBackend is storage that cannot be read at all, like a browser with
// local storage disabled.
type lockedBackend struct{ chaosBackend }

func (*lockedBackend) Read(string) (string, bool, error) {
	return "", false, storage.ErrStorageUnavailable
}

func TestSaveBytesIfSeqRefusesASaveThatChangedSinceLoad(t *testing.T) {
	s, _ := newSlots(newChaos())
	seq, err := s.SaveBytesIfSeq("career", 0, SaveAuto, "", []byte(`{"week":1}`))
	if err != nil || seq != 1 {
		t.Fatalf("first save: seq %d, %v", seq, err)
	}
	// Another tab saves on top of seq 1.
	if _, err := s.SaveBytesIfSeq("career", 1, SaveAuto, "", []byte(`{"week":2}`)); err != nil {
		t.Fatal(err)
	}
	// This tab still holds seq 1: its save must not land.
	if _, err := s.SaveBytesIfSeq("career", 1, SaveAuto, "", []byte(`{"week":9}`)); !errors.Is(err, ErrSaveConflict) {
		t.Fatalf("stale save: %v, want ErrSaveConflict", err)
	}
	raw, info, err := s.LoadBytes("career")
	if err != nil || string(raw) != `{"week":2}` || info.Seq != 2 {
		t.Fatalf("loaded %s seq %d (%v), want the other tab's week 2", raw, info.Seq, err)
	}
}

func TestUnreadableStorageIsAnErrorNotAnEmptySlot(t *testing.T) {
	s, _ := newSlots(&lockedBackend{chaosBackend: *newChaos()})
	if _, _, err := s.LoadBytes("career"); !errors.Is(err, ErrSaveUnreadable) || !errors.Is(err, storage.ErrStorageUnavailable) {
		t.Fatalf("load from locked storage: %v", err)
	}
	if err := s.SaveBytes("career", SaveAuto, "", []byte(`{}`)); !errors.Is(err, ErrSaveUnreadable) {
		t.Fatalf("save over unreadable storage: %v, want a refusal", err)
	}
}
