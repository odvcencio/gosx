package game

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type health struct{ HP int }
type calendar struct{ Day int }

func newTestCodec(t *testing.T) *SaveCodec {
	t.Helper()
	c := NewSaveCodec()
	for _, err := range []error{
		RegisterComponent[Transform](c, "transform"),
		RegisterComponent[health](c, "health", ValueCodec[health]{
			Marshal: func(h health) ([]byte, error) { return []byte(`{"hp":` + itoa(h.HP) + `}`), nil },
			Unmarshal: func(b []byte) (health, error) {
				var v struct {
					HP int `json:"hp"`
				}
				err := json.Unmarshal(b, &v)
				return health{v.HP}, err
			},
		}),
		RegisterResource[calendar](c, "calendar"),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func buildWorld() (*World, EntityID, EntityID) {
	w := NewWorld()
	a := w.Spawn(WithName("hero"))
	b := w.Spawn()
	gone := w.Spawn()
	c := w.Spawn(WithName("tower"))
	w.Despawn(gone)
	SetComponent(w, a, Transform{Position: Vec3{X: 1, Y: 2, Z: 3}})
	SetComponent(w, c, Transform{Position: Vec3{X: 9}})
	SetComponent(w, b, health{HP: 7})
	w.Set(c, health{HP: 100}) // boxed path
	SetResource(w, calendar{Day: 41})
	return w, a, c
}

func TestSaveCodecRoundTripsWorld(t *testing.T) {
	codec := newTestCodec(t)
	w, hero, tower := buildWorld()
	save, err := codec.ExportWorld(w)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(save)
	again, _ := codec.ExportWorld(w)
	data2, _ := json.Marshal(again)
	if !bytes.Equal(data, data2) {
		t.Fatal("export is not deterministic")
	}

	var loaded WorldSave
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	dst := NewWorld()
	SetResource(dst, struct{ Stale int }{1}) // unregistered leftovers are cleared
	if err := codec.ImportWorld(dst, loaded); err != nil {
		t.Fatal(err)
	}
	if got, ok := dst.Entity("hero"); !ok || got != hero {
		t.Fatalf("hero %v %v", got, ok)
	}
	if got, ok := GetComponent[Transform](dst, tower); !ok || got.Position.X != 9 {
		t.Fatalf("tower transform %+v %v", got, ok)
	}
	if got, ok := GetComponent[health](dst, tower); !ok || got.HP != 100 {
		t.Fatalf("tower health %+v", got)
	}
	if cal, _ := GetResource[calendar](dst); cal.Day != 41 {
		t.Fatalf("calendar %+v", cal)
	}
	if _, ok := GetResource[struct{ Stale int }](dst); ok {
		t.Fatal("stale resource survived import")
	}
	if len(dst.Entities()) != 3 {
		t.Fatalf("entities %v", dst.Entities())
	}
	// IDs are not reused: the despawned entity's number stays retired.
	if next := dst.Spawn(); next != tower+1 {
		t.Fatalf("next id %d want %d", next, tower+1)
	}
}

func TestImportWorldFailureLeavesWorldUntouched(t *testing.T) {
	codec := newTestCodec(t)
	w, _, _ := buildWorld()
	save, _ := codec.ExportWorld(w)
	save.Columns[0].Values[0] = json.RawMessage(`"not an object"`)
	dst, hero, _ := buildWorld()
	before := len(dst.Entities())
	if err := codec.ImportWorld(dst, save); err == nil {
		t.Fatal("expected decode error")
	}
	if len(dst.Entities()) != before || !dst.Alive(hero) {
		t.Fatal("failed import modified the world")
	}
}

func TestImportWorldUnknownNamesAndBadInput(t *testing.T) {
	codec := newTestCodec(t)
	w, _, _ := buildWorld()
	save, _ := codec.ExportWorld(w)
	save.Columns = append(save.Columns, ColumnSave{Name: "from-the-future", Entities: []EntityID{1}, Values: []json.RawMessage{json.RawMessage(`1`)}})
	if err := codec.ImportWorld(NewWorld(), save); err == nil {
		t.Fatal("unknown component accepted")
	}
	codec.SkipUnknown = true
	if err := codec.ImportWorld(NewWorld(), save); err != nil {
		t.Fatal(err)
	}
	bad := save
	bad.Version = 99
	if err := codec.ImportWorld(NewWorld(), bad); err == nil {
		t.Fatal("bad version accepted")
	}
	if err := RegisterComponent[health](codec, "other"); err == nil {
		t.Fatal("duplicate type accepted")
	}
	if err := RegisterComponent[Velocity](codec, "health"); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if err := RegisterResource[calendar](nil, "x"); err == nil {
		t.Fatal("nil codec accepted")
	}
	boom := errors.New("boom")
	c2 := NewSaveCodec()
	_ = RegisterComponent[health](c2, "h", ValueCodec[health]{Marshal: func(health) ([]byte, error) { return nil, boom }})
	if _, err := c2.ExportWorld(w); !errors.Is(err, boom) {
		t.Fatalf("export error %v", err)
	}
}

func TestSaveCodecSnapshotRestoresWorldClockAndSettings(t *testing.T) {
	codec := newTestCodec(t)
	w, _, tower := buildWorld()
	rt := New(Config{World: w, FixedStep: time.Hour, Snapshot: codec.SnapshotCodec()})
	rt.SetTimeScale(3)
	for i := 0; i < 5; i++ {
		rt.Tick(nil)
	}
	snap := rt.Snapshot()

	other := New(Config{FixedStep: time.Hour, Snapshot: codec.SnapshotCodec()})
	other.Restore(snap)
	if other.Frame().Time != rt.Frame().Time || other.TimeScale() != 3 {
		t.Fatalf("clock: %v scale %v", other.Frame().Time, other.TimeScale())
	}
	if got, ok := GetComponent[health](other.World(), tower); !ok || got.HP != 100 {
		t.Fatalf("world not restored: %+v %v", got, ok)
	}
	if bytes.Contains(rt.State(), []byte(`"world"`)) {
		t.Fatal("State should leave the world out by default")
	}
	codec.IncludeWorldInState = true
	if !bytes.Contains(rt.State(), []byte(`"world"`)) {
		t.Fatal("IncludeWorldInState ignored")
	}
}
