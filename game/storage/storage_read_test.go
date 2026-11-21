package storage

import (
	"errors"
	"testing"
)

type failingReader struct{ *MemoryBackend }

func (failingReader) Read(string) (string, bool, error) { return "", false, ErrStorageUnavailable }

func TestReadReportsAnUnreadableBackend(t *testing.T) {
	mem := NewMemoryBackend()
	n := New("game", mem)
	_ = n.Set("k", "v")
	if v, ok, err := n.Read("k"); v != "v" || !ok || err != nil {
		t.Fatalf("Read = %q %v %v", v, ok, err)
	}
	if _, _, err := New("game", failingReader{mem}).Read("k"); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("a Reader's error was dropped: %v", err)
	}
	if _, _, err := New("game", nil).Read("k"); !errors.Is(err, ErrNoBackend) {
		t.Fatalf("nil backend: %v", err)
	}
}
