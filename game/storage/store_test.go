package storage

import (
	"errors"
	"testing"
)

func TestNamespacedGetSetDelete(t *testing.T) {
	n := New("room-1", NewMemoryBackend())
	if _, ok := n.Get("resume"); ok {
		t.Fatal("expected miss before Set")
	}
	if err := n.Set("resume", "tok-abc"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, ok := n.Get("resume"); !ok || v != "tok-abc" {
		t.Fatalf("Get = %q ok=%v, want tok-abc/true", v, ok)
	}
	if err := n.Delete("resume"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := n.Get("resume"); ok {
		t.Fatal("expected miss after Delete")
	}
}

func TestNamespacedPrefixIsolation(t *testing.T) {
	backend := NewMemoryBackend()
	a := New("room-a", backend)
	b := New("room-b", backend)
	if err := a.Set("resume", "a-token"); err != nil {
		t.Fatalf("Set a: %v", err)
	}
	if err := b.Set("resume", "b-token"); err != nil {
		t.Fatalf("Set b: %v", err)
	}
	if v, ok := a.Get("resume"); !ok || v != "a-token" {
		t.Fatalf("a.Get = %q ok=%v, want a-token/true", v, ok)
	}
	if v, ok := b.Get("resume"); !ok || v != "b-token" {
		t.Fatalf("b.Get = %q ok=%v, want b-token/true", v, ok)
	}
	// The raw backend key carries the namespace prefix.
	if raw, ok := backend.Get("room-a.resume"); !ok || raw != "a-token" {
		t.Fatalf("raw backend key room-a.resume = %q ok=%v, want a-token/true", raw, ok)
	}
}

func TestNamespacedJSONRoundTrip(t *testing.T) {
	type settings struct {
		Volume int    `json:"volume"`
		Name   string `json:"name"`
	}
	n := New("settings", NewMemoryBackend())
	if err := SetJSON(n, "audio", settings{Volume: 7, Name: "pilot"}); err != nil {
		t.Fatalf("SetJSON: %v", err)
	}
	got, ok := GetJSON[settings](n, "audio")
	if !ok {
		t.Fatal("GetJSON miss after SetJSON")
	}
	if got.Volume != 7 || got.Name != "pilot" {
		t.Fatalf("GetJSON = %+v, want {7 pilot}", got)
	}
}

func TestNamespacedGetJSONMissOnDecodeFailure(t *testing.T) {
	backend := NewMemoryBackend()
	_ = backend.Set("ns.audio", "not json")
	n := New("ns", backend)
	if _, ok := GetJSON[int](n, "audio"); ok {
		t.Fatal("expected decode failure to report a miss")
	}
}

// failingBackend simulates a full quota or a disabled localStorage: every
// write fails, but nothing panics.
type failingBackend struct{}

func (failingBackend) Get(string) (string, bool) { return "", false }
func (failingBackend) Set(string, string) error  { return errors.New("quota exceeded") }
func (failingBackend) Delete(string) error       { return errors.New("quota exceeded") }

func TestNamespacedGracefulFailureOnBackendError(t *testing.T) {
	n := New("room-1", failingBackend{})
	if err := n.Set("resume", "tok"); err == nil {
		t.Fatal("expected Set to surface the backend error")
	}
	if _, ok := n.Get("resume"); ok {
		t.Fatal("expected Get to miss cleanly, not panic")
	}
}

func TestNilBackendIsGraceful(t *testing.T) {
	n := New("room-1", nil)
	if _, ok := n.Get("resume"); ok {
		t.Fatal("expected miss with a nil backend")
	}
	if err := n.Set("resume", "tok"); !errors.Is(err, ErrNoBackend) {
		t.Fatalf("Set err = %v, want ErrNoBackend", err)
	}
	if err := n.Delete("resume"); !errors.Is(err, ErrNoBackend) {
		t.Fatalf("Delete err = %v, want ErrNoBackend", err)
	}
}

func TestNewDefaultUsesPlatformBackend(t *testing.T) {
	n := NewDefault("room-1")
	if err := n.Set("resume", "tok"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, ok := n.Get("resume"); !ok || v != "tok" {
		t.Fatalf("Get = %q ok=%v, want tok/true", v, ok)
	}
}
