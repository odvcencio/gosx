package settings

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/storage"
)

func newStore() *storage.Namespaced { return storage.New("t", storage.NewMemoryBackend()) }

func TestDefaultsAreValid(t *testing.T) {
	d := Defaults(1)
	if !reflect.DeepEqual(d, d.Valid()) {
		t.Fatalf("defaults change under Valid:\n%+v\n%+v", d, d.Valid())
	}
}

func TestValidClamps(t *testing.T) {
	s := Defaults(1)
	s.Volume.Master = -3
	s.Volume.Music = 9
	s.Volume.SFX = math.NaN()
	s.CameraShake = math.Inf(1)
	s.RenderScale = 0
	s.FrameCap = 5
	s.Quality = "ludicrous"
	s.Motion = "maybe"
	s.Language = "  "
	v := s.Valid()
	if v.Volume.Master != 0 || v.Volume.Music != 1 || v.Volume.SFX != Defaults(1).Volume.SFX {
		t.Errorf("volumes %+v", v.Volume)
	}
	if v.CameraShake != 1 || v.RenderScale != 0.25 || v.FrameCap != 15 || v.Quality != QualityAuto || v.Motion != MotionSystem || v.Language != "en" {
		t.Errorf("%+v", v)
	}
	v.FrameCap = 0
	if v.Valid().FrameCap != 0 {
		t.Error("0 means uncapped")
	}
}

func TestVolumesByBus(t *testing.T) {
	v := Defaults(1).Volume
	for i, b := range Buses {
		v = v.With(b, float64(i)/10)
	}
	for i, b := range Buses {
		if v.Get(b) != float64(i)/10 {
			t.Errorf("%s = %v", b, v.Get(b))
		}
	}
	if v.With("bogus", 0.3) != v || v.Get("bogus") != 1 {
		t.Error("unknown bus")
	}
}

func TestRoundTripAndHooks(t *testing.T) {
	store := newStore()
	m := NewManager(Config{Store: store})
	var got []Field
	var vol Volumes
	m.OnChange(FieldVolume, func(s Settings, f Field) { got = append(got, f); vol = s.Volume })
	m.OnChange(FieldAll, func(s Settings, f Field) {})
	if _, err := m.SetVolume(BusMusic, 0.25); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != FieldVolume || vol.Music != 0.25 {
		t.Fatalf("hook %v %+v", got, vol)
	}
	if ch, _ := m.SetVolume(BusMusic, 0.25); ch != 0 || len(got) != 1 {
		t.Fatal("no-op update fired hooks")
	}
	m.Update(func(s *Settings) {
		s.RenderScale = 0.75
		s.FrameCap = 60
		s.Language = "de"
		s.Extra = map[string]json.RawMessage{"fov": json.RawMessage(`90`)}
	})
	if len(got) != 1 {
		t.Fatal("volume hook must not run for other fields")
	}

	m2 := NewManager(Config{Store: store})
	var loadedFields Field
	m2.OnChange(FieldAll, func(_ Settings, f Field) { loadedFields |= f })
	ok, err := m2.Load()
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	if !reflect.DeepEqual(m.Get(), m2.Get()) {
		t.Fatalf("round trip differs:\n%+v\n%+v", m.Get(), m2.Get())
	}
	if loadedFields&(FieldVolume|FieldRenderScale|FieldFrameCap|FieldLanguage|FieldExtra) == 0 {
		t.Fatalf("load fired %b", loadedFields)
	}
	// Get returns a copy.
	g := m2.Get()
	g.Extra["fov"] = json.RawMessage(`1`)
	if string(m2.Get().Extra["fov"]) != "90" {
		t.Fatal("Get leaked internal map")
	}
}

func TestBindingsStored(t *testing.T) {
	store := newStore()
	m := NewManager(Config{Store: store})
	am := actions.NewMap()
	am.Define(actions.Context{Name: "g", Actions: []actions.ActionDef{{Name: "jump", Bindings: []actions.Binding{{Source: actions.Key("Space")}}}}})
	if _, err := am.Rebind("g", "jump", 0, actions.Key("KeyJ"), actions.ConflictReject); err != nil {
		t.Fatal(err)
	}
	ch, err := m.SetBindings(am.Export())
	if err != nil || ch != FieldBindings {
		t.Fatal(ch, err)
	}
	m2 := NewManager(Config{Store: store})
	if _, err := m2.Load(); err != nil {
		t.Fatal(err)
	}
	am2 := actions.NewMap()
	am2.Define(actions.Context{Name: "g", Actions: []actions.ActionDef{{Name: "jump", Bindings: []actions.Binding{{Source: actions.Key("Space")}}}}})
	if n, err := am2.Import(m2.Get().Bindings); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if am2.Bindings("g", "jump")[0].Source != actions.Key("KeyJ") {
		t.Fatal("binding lost")
	}
}

func TestMigration(t *testing.T) {
	store := newStore()
	// Version 1 stored volumes as percentages under "volumes".
	old := `{"version":1,"volumes":{"master":50,"music":25},"renderScale":0.5,"language":"fr"}`
	_ = store.Set("settings", old)
	m := NewManager(Config{Store: store, Version: 2, Migrations: []Migration{{From: 1, Up: func(doc map[string]json.RawMessage) error {
		var pct map[string]float64
		if err := json.Unmarshal(doc["volumes"], &pct); err != nil {
			return err
		}
		out := map[string]float64{}
		for k, v := range pct {
			out[k] = v / 100
		}
		delete(doc, "volumes")
		doc["volume"], _ = json.Marshal(out)
		return nil
	}}}})
	ok, err := m.Load()
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	s := m.Get()
	if s.Version != 2 || s.Volume.Master != 0.5 || s.Volume.Music != 0.25 || s.Volume.SFX != Defaults(2).Volume.SFX || s.RenderScale != 0.5 || s.Language != "fr" {
		t.Fatalf("%+v", s)
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	raw, _ := store.Get("settings")
	var chk struct{ Version int }
	_ = json.Unmarshal([]byte(raw), &chk)
	if chk.Version != 2 {
		t.Fatal("saved version", chk.Version)
	}
	// Missing step is corrupt, not a silent default.
	_ = store.Set("settings", `{"version":1}`)
	m3 := NewManager(Config{Store: store, Version: 3})
	if _, err := m3.Load(); !errors.Is(err, ErrCorrupt) {
		t.Fatal(err)
	}
	// Unversioned documents are version 1.
	_ = store.Set("settings", `{"renderScale":0.5}`)
	m4 := NewManager(Config{Store: store})
	if _, err := m4.Load(); err != nil || m4.Get().RenderScale != 0.5 {
		t.Fatal(err, m4.Get().RenderScale)
	}
}

func TestNewerDocumentIsNotOverwritten(t *testing.T) {
	store := newStore()
	future := `{"version":9,"renderScale":0.5,"futureField":true}`
	_ = store.Set("settings", future)
	m := NewManager(Config{Store: store})
	if ok, err := m.Load(); !ok || !errors.Is(err, ErrTooNew) {
		t.Fatal(ok, err)
	}
	if _, err := m.SetVolume(BusMaster, 0.1); !errors.Is(err, ErrTooNew) {
		t.Fatalf("save should refuse: %v", err)
	}
	if m.Get().Volume.Master != 0.1 {
		t.Fatal("in-memory change should still apply")
	}
	if raw, _ := store.Get("settings"); raw != future {
		t.Fatal("newer document was overwritten")
	}
}

func TestCorruptDocument(t *testing.T) {
	store := newStore()
	_ = store.Set("settings", "{not json")
	m := NewManager(Config{Store: store})
	if ok, err := m.Load(); !ok || !errors.Is(err, ErrCorrupt) {
		t.Fatal(ok, err)
	}
	if !reflect.DeepEqual(m.Get(), NewManager(Config{}).Get()) {
		t.Fatal("corrupt load changed settings")
	}
	if _, err := m.SetVolume(BusSFX, 0.5); err != nil {
		t.Fatal("a corrupt document should be replaced:", err)
	}
	// Hostile values are clamped on load.
	_ = store.Set("settings", `{"volume":{"master":5},"renderScale":-4,"quality":"x","frameCap":100000}`)
	m2 := NewManager(Config{Store: store})
	if _, err := m2.Load(); err != nil {
		t.Fatal(err)
	}
	s := m2.Get()
	if s.Volume.Master != 1 || s.RenderScale != 0.25 || s.Quality != QualityAuto || s.FrameCap != 360 {
		t.Fatalf("%+v", s)
	}
}

func TestReducedMotionAndShake(t *testing.T) {
	m := NewManager(Config{})
	var n int
	m.OnChange(FieldMotion, func(Settings, Field) { n++ })
	if m.ReducedMotion() || m.ShakeScale() != 1 {
		t.Fatal("default")
	}
	m.SetSystemReducedMotion(true)
	if !m.ReducedMotion() || m.ShakeScale() != 0 || n != 1 {
		t.Fatalf("system on: %v %v %d", m.ReducedMotion(), m.ShakeScale(), n)
	}
	m.Update(func(s *Settings) { s.Motion = MotionFull })
	if m.ReducedMotion() || m.ShakeScale() != 1 {
		t.Fatal("full overrides system")
	}
	m.SetSystemReducedMotion(false)
	m.Update(func(s *Settings) { s.Motion = MotionReduce; s.CameraShake = 0.5 })
	if !m.ReducedMotion() || m.ShakeScale() != 0 {
		t.Fatal("reduce")
	}
	m.Update(func(s *Settings) { s.Motion = MotionSystem })
	if m.ShakeScale() != 0.5 {
		t.Fatal("shake slider", m.ShakeScale())
	}
}

func TestLanguagesAndReset(t *testing.T) {
	m := NewManager(Config{Languages: []string{"en", "de"}})
	m.Update(func(s *Settings) { s.Language = "xx" })
	if m.Get().Language != "en" {
		t.Fatal(m.Get().Language)
	}
	m.Update(func(s *Settings) { s.Language = "de"; s.RenderScale = 0.5 })
	m.Reset()
	if g := m.Get(); g.Language != "de" || g.RenderScale != 1 {
		t.Fatalf("%+v", g)
	}
}
