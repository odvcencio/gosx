package game

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/game/storage"
)

type chaosBackend struct {
	mu    sync.Mutex
	m     map[string]string
	tear  int // truncate the nth Set from now (1-based); 0 off
	fail  int // fail the nth Set with an error
	count int
}

func newChaos() *chaosBackend { return &chaosBackend{m: map[string]string{}} }
func (b *chaosBackend) Get(k string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.m[k]
	return v, ok
}
func (b *chaosBackend) Set(k, v string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.count++
	if b.tear != 0 && b.count == b.tear {
		b.m[k] = v[:len(v)/2]
		return nil
	}
	if b.fail != 0 && b.count == b.fail {
		return errors.New("quota exceeded")
	}
	b.m[k] = v
	return nil
}
func (b *chaosBackend) Delete(k string) error { delete(b.m, k); return nil }

type save struct {
	Level int    `json:"level"`
	Name  string `json:"name"`
}

func newSlots(b storage.Backend, mods ...func(*SlotConfig)) (*SlotStore, *time.Time) {
	now := time.Unix(1_700_000_000, 0)
	cfg := SlotConfig{Store: storage.New("g", b), Now: func() time.Time { return now }}
	for _, m := range mods {
		m(&cfg)
	}
	return NewSlotStore(cfg), &now
}

func TestSlotRoundTripAndAlternation(t *testing.T) {
	b := newChaos()
	s, now := newSlots(b)
	for i := 1; i <= 5; i++ {
		*now = now.Add(time.Minute)
		if err := s.Save("slot1", SaveManual, "level", save{Level: i}); err != nil {
			t.Fatal(err)
		}
		got, info, err := LoadSave[save](s, "slot1")
		if err != nil || got.Level != i || info.Seq != uint64(i) || info.Kind != SaveManual || info.Label != "level" {
			t.Fatalf("i=%d: %+v %+v %v", i, got, info, err)
		}
	}
	// Both physical copies exist and hold the two newest saves.
	a, _ := b.Get("g.save.slot1.a")
	bb, _ := b.Get("g.save.slot1.b")
	if a == "" || bb == "" || a == bb {
		t.Fatal("two copies expected")
	}
	if _, _, err := LoadSave[save](s, "nothing"); !errors.Is(err, ErrNoSave) {
		t.Fatal(err)
	}
}

func TestTornWriteKeepsPreviousSave(t *testing.T) {
	for tear := 0; tear < 6; tear++ {
		b := newChaos()
		s, _ := newSlots(b)
		for i := 1; i <= tear+1; i++ {
			if err := s.Save("auto", SaveAuto, "", save{Level: i}); err != nil {
				t.Fatal(err)
			}
		}
		last := tear + 1
		b.count, b.tear = 0, 1
		_ = s.Save("auto", SaveAuto, "", save{Level: 99}) // torn
		got, _, err := LoadSave[save](s, "auto")
		if last == 1 {
			// Only one earlier save existed; it must still be readable.
			if err != nil || got.Level != 1 {
				t.Fatalf("tear after %d: %+v %v", last, got, err)
			}
			continue
		}
		if err != nil || got.Level != last {
			t.Fatalf("tear after %d saves: got %+v err %v, want level %d", last, got, err, last)
		}
	}
}

func TestFailedWriteKeepsPreviousSave(t *testing.T) {
	b := newChaos()
	s, _ := newSlots(b)
	_ = s.Save("auto", SaveAuto, "", save{Level: 1})
	_ = s.Save("auto", SaveAuto, "", save{Level: 2})
	b.count, b.fail = 0, 1
	if err := s.Save("auto", SaveAuto, "", save{Level: 3}); err == nil {
		t.Fatal("expected write error")
	}
	if got, _, _ := LoadSave[save](s, "auto"); got.Level != 2 {
		t.Fatalf("level %d", got.Level)
	}
}

func TestBitRotInPayloadIsDetected(t *testing.T) {
	b := newChaos()
	s, _ := newSlots(b)
	_ = s.Save("auto", SaveAuto, "", save{Level: 1, Name: "aaaa"})
	_ = s.Save("auto", SaveAuto, "", save{Level: 2, Name: "bbbb"})
	// Flip a payload byte in the newest copy (it still parses as JSON).
	for _, k := range []string{"g.save.auto.a", "g.save.auto.b"} {
		v, _ := b.Get(k)
		if strings.Contains(v, `"level":2`) {
			b.m[k] = strings.Replace(v, `"bbbb"`, `"bbbc"`, 1)
		}
	}
	got, info, err := LoadSave[save](s, "auto")
	if err != nil || got.Level != 1 || info.Seq != 1 {
		t.Fatalf("expected fallback to the older valid copy: %+v %+v %v", got, info, err)
	}
	// Corrupt both.
	b.m["g.save.auto.a"] = "{"
	b.m["g.save.auto.b"] = "garbage"
	if _, _, err := s.LoadBytes("auto"); !errors.Is(err, ErrSaveCorrupt) {
		t.Fatal(err)
	}
	// A fresh save overwrites garbage and works again.
	if err := s.Save("auto", SaveAuto, "", save{Level: 7}); err != nil {
		t.Fatal(err)
	}
	if got, _, err := LoadSave[save](s, "auto"); err != nil || got.Level != 7 {
		t.Fatal(got, err)
	}
}

func TestSaveMigrationAndTooNew(t *testing.T) {
	b := newChaos()
	v1, _ := newSlots(b)
	_ = v1.Save("slot1", SaveManual, "", map[string]any{"hp": 10})
	v2, _ := newSlots(b, func(c *SlotConfig) {
		c.Version = 2
		c.Migrations = []SaveMigration{{From: 1, Up: func(p json.RawMessage) (json.RawMessage, error) {
			var m map[string]any
			if err := json.Unmarshal(p, &m); err != nil {
				return nil, err
			}
			m["health"], m["hp"] = m["hp"], nil
			delete(m, "hp")
			return json.Marshal(m)
		}}}
	})
	got, info, err := LoadSave[map[string]any](v2, "slot1")
	if err != nil || got["health"] != float64(10) || info.Version != 2 {
		t.Fatalf("%v %+v %v", got, info, err)
	}
	// Saving from v2, then reading with v1, is refused.
	_ = v2.Save("slot2", SaveManual, "", map[string]any{"health": 5})
	if _, _, err := v1.LoadBytes("slot2"); !errors.Is(err, ErrSaveTooNew) {
		t.Fatal(err)
	}
	// Missing migration step is an error, not a silent pass.
	v3, _ := newSlots(b, func(c *SlotConfig) { c.Version = 3 })
	if _, _, err := v3.LoadBytes("slot1"); err == nil || !strings.Contains(err.Error(), "no save migration") {
		t.Fatal(err)
	}
}

func TestListInfoDeleteAndNames(t *testing.T) {
	s, now := newSlots(newChaos())
	*now = now.Add(time.Hour)
	_ = s.Save("slot1", SaveManual, "forest", save{Level: 1})
	*now = now.Add(time.Hour)
	_ = s.Save("auto", SaveAuto, "cave", save{Level: 2})
	list := s.List("slot1", "slot2", "auto")
	if len(list) != 2 || list[0].Name != "auto" || list[1].Label != "forest" {
		t.Fatalf("%+v", list)
	}
	if !list[0].SavedAt.Equal(*now) {
		t.Fatal("savedAt")
	}
	if err := s.Delete("auto"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Info("auto"); ok {
		t.Fatal("not deleted")
	}
	for _, bad := range []string{"", "a.b", "a b", "../x"} {
		if err := s.Save(bad, SaveAuto, "", 1); err == nil {
			t.Errorf("name %q should be rejected", bad)
		}
	}
	if err := s.SaveBytes("x", SaveAuto, "", []byte("{nope")); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	var nilStore *SlotStore
	if err := nilStore.Save("x", SaveAuto, "", 1); !errors.Is(err, storage.ErrNoBackend) {
		t.Fatal(err)
	}
}

func TestAutosaverInterval(t *testing.T) {
	s, _ := newSlots(newChaos())
	n, allow := 0, false
	a := &Autosaver{Store: s, Interval: 10 * time.Second, Capture: func() ([]byte, string, bool) {
		n++
		return []byte(`{"n":1}`), "run", allow
	}}
	for i := 0; i < 9; i++ {
		if a.Update(time.Second) {
			t.Fatal("early")
		}
	}
	if a.Update(time.Second) || n != 1 {
		t.Fatal("capture refused: no save, retry next frame")
	}
	allow = true
	if !a.Update(16*time.Millisecond) || n != 2 {
		t.Fatal("should save once allowed")
	}
	if info, ok := s.Info("auto"); !ok || info.Kind != SaveAuto || info.Label != "run" {
		t.Fatalf("%+v", info)
	}
	if a.Update(time.Second) {
		t.Fatal("timer must restart")
	}
}

func TestSaveRuntimeRoundTrip(t *testing.T) {
	codec := NewSaveCodec()
	type hp struct{ N int }
	if err := RegisterComponent[hp](codec, "hp"); err != nil {
		t.Fatal(err)
	}
	r := New(Config{Snapshot: codec.SnapshotCodec()})
	e := r.World().Spawn()
	SetComponent(r.World(), e, hp{N: 42})
	s, _ := newSlots(newChaos())
	if err := s.SaveRuntime("slot1", SaveManual, "", r); err != nil {
		t.Fatal(err)
	}
	r2 := New(Config{Snapshot: codec.SnapshotCodec()})
	if err := s.LoadRuntime("slot1", r2); err != nil {
		t.Fatal(err)
	}
	if v, ok := GetComponent[hp](r2.World(), e); !ok || v.N != 42 {
		t.Fatalf("%v %v", v, ok)
	}
}
