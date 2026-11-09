package teamai

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestLoadJSONRoundTripRunsTheSame(t *testing.T) {
	data, err := json.MarshalIndent(ctfSpec(5), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadJSON(data, nil)
	if err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}
	a := newDrill(t, 5, 8)
	b := newDrillBook(t, loaded, 5, 8)
	for i := 0; i < 400; i++ {
		a.step()
		b.step()
		if a.hash() != b.hash() {
			t.Fatalf("JSON-loaded book diverges from the Go-built one at tick %d", i)
		}
	}
}

func TestDecodeJSONErrors(t *testing.T) {
	const head = `"format": "gosx.teamai", "version": 1, "roles": ["a"]`
	cases := []struct {
		name string
		data string
		want string
	}{
		{"syntax error has line and column",
			"{\n" + head + ",\n  \"formations\": [ {\"name\": \"f\",, } ]\n}",
			"line 3 column 32: JSON syntax"},
		{"unknown field with a hint",
			`{` + head + `, "formations": [{"name": "wedge", "slots": [{"name": "w", "rol": "a"}]}]}`,
			`formations[0](wedge).slots[0](w).rol: unknown field "rol" (did you mean "role"?)`},
		{"wrong type",
			`{` + head + `, "paths": [{"name": "p", "segments": [{"kind": "line", "label": "go", "speed": "fast"}]}]}`,
			`paths[0](p).segments[0](go).speed: want a number`},
		{"bad point",
			`{` + head + `, "landmarks": [{"name": "spot", "at": [1, 2, 3]}]}`,
			`landmarks[0](spot).at: want a two-number array [x, y], got 3 numbers`},
		{"bad side",
			`{` + head + `, "landmarks": [{"name": "spot", "side": "left"}]}`,
			`landmarks[0](spot).side: side "left": want fixed, strong or weak`},
		{"duplicate key",
			`{` + head + `, "bands": ["x"], "bands": ["y"]}`,
			`bands: duplicate key`},
		{"unknown top-level field",
			`{` + head + `, "playbook": []}`,
			`playbook: unknown field "playbook"`},
		{"embedded decision fields are known",
			`{` + head + `, "paths": [{"name": "p", "segments": [{"kind": "decide", "windw": 1}]}]}`,
			`paths[0](p).segments[0].windw: unknown field "windw" (did you mean "window"?)`},
		{"newer version",
			`{"format": "gosx.teamai", "version": 2, "future": true}`,
			`version: version 2 is newer than this build reads (1)`},
		{"wrong format",
			`{"format": "other", "version": 1}`,
			`format: format "other": want "gosx.teamai"`},
		{"trailing data",
			`{` + head + `} {}`,
			`data after the top-level object`},
		{"not an object",
			`[1, 2]`,
			`a teamai data file is a JSON object`},
		{"truncated",
			`{` + head,
			`unexpected end of input`},
		{"whole number",
			`{"format": "gosx.teamai", "version": 1.5}`,
			`version: want a whole number`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DecodeJSON([]byte(c.data))
			var se *SpecError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v, want a *SpecError", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q\nwant it to contain %q", err, c.want)
			}
		})
	}
}

func TestLoadJSONReportsCompileErrorsByPath(t *testing.T) {
	data := `{
	  "format": "gosx.teamai", "version": 1,
	  "roles": ["a"],
	  "formations": [{"name": "line", "slots": [{"name": "s0", "role": "b"}]}],
	  "assignments": [{"name": "x", "steps": [{"do": "hold", "until": "never"}]}]
	}`
	_, err := LoadJSON([]byte(data), nil)
	var se *SpecError
	if !errors.As(err, &se) || len(se.Issues) != 2 {
		t.Fatalf("err = %v, want two issues", err)
	}
	want := []string{
		`formations[0](line).slots[0](s0).role: unknown role "b"`,
		`assignments[0](x).steps[0].until: unknown rule "never": not in rules and not registered`,
	}
	for i, w := range want {
		if got := se.Issues[i].String(); got != w {
			t.Errorf("issue %d = %q, want %q", i, got, w)
		}
	}
}

// FuzzLoadJSON feeds arbitrary bytes to LoadJSON. It must never panic, and a
// file that loads must run in an executor without panicking.
func FuzzLoadJSON(f *testing.F) {
	full, _ := json.Marshal(ctfSpec(3))
	f.Add(full)
	f.Add([]byte(`{"format":"gosx.teamai","version":1,"roles":["a"]}`))
	f.Add([]byte(`{"format":"gosx.teamai","version":1,"roles":["a"],"formations":[{"name":"f","slots":[{"name":"s","role":"a","from":"s"}]}]}`))
	f.Add([]byte(`{"format":"gosx.teamai","version":1,"roles":["a"],"paths":[{"name":"p","segments":[{"label":"l","kind":"decide","branches":[{"goto":"l"}]}]}],"assignments":[{"name":"x","steps":[{"do":"path","path":"p"}]}]}`))
	f.Add([]byte(`{"format":"gosx.teamai","version":1,"roles":["a"],"rules":[{"name":"r","fact":"self.dist","op":"<","value":1e308}]}`))
	f.Add([]byte(`{"format":"gosx.teamai","version":1,"roles":["a"],"landmarks":[{"name":"l","at":[1e308,-1e308]}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		b, err := LoadJSON(data, nil)
		if err != nil {
			var se *SpecError
			if !errors.As(err, &se) {
				t.Fatalf("error is %T, want *SpecError: %v", err, err)
			}
			return
		}
		exerciseBook(t, b)
	})
}

// exerciseBook runs every plan and every assignment of b for a short while in
// a world shaped from the Book.
func exerciseBook(t *testing.T, b *Book) {
	ex, err := NewExecutor(b, Options{Step: testDT, Separation: 1})
	if err != nil {
		t.Fatal(err)
	}
	roles := b.NumRoles()
	if roles == 0 {
		roles = 1
	}
	n := 2 * roles
	if n > 12 {
		n = 12
	}
	w := World{
		Field:        Rect{Min: V2(-30, -30), Max: V2(30, 30)},
		Teams:        []Team{{Frame: Frame{Band: 0}}, {Frame: Frame{Forward: V2(0, -1), Origin: V2(0, 10), Band: -1}}},
		Agents:       make([]Body, n),
		Objects:      make([]Body, b.NumObjects()),
		Landmarks:    make([]Vec2, b.NumLandmarks()),
		Signals:      make([]float64, b.NumSignals()),
		TeamSignals:  make([]float64, 2*b.NumTeamSignals()),
		AgentSignals: make([]float64, n*b.NumAgentSignals()),
	}
	for i := range w.Agents {
		w.Agents[i] = Body{Pos: V2(float64(i), float64(i%3)), Team: i % 2, Role: RoleID(i % roles),
			MaxSpeed: 6, MaxAccel: 10, Reaction: uint16(i % 3)}
	}
	ex.Reset(n, 2)
	for p := 0; p < b.NumPlans(); p++ {
		if _, err := ex.StartPlan(&w, p%2, PlanID(p)); err != nil {
			t.Fatal(err)
		}
	}
	for a := 0; a < b.NumAssignments() && a < n; a++ {
		ex.Assign(a, AssignmentID(a))
	}
	for tick := 0; tick < 40; tick++ {
		cmds := ex.Step(&w)
		for i := range w.Agents {
			Integrate(&w.Agents[i], cmds[i].Vel, testDT)
		}
		w.Tick++
	}
	snap := ex.AppendState(nil)
	if err := ex.LoadState(snap); err != nil {
		t.Fatalf("own snapshot does not load: %v", err)
	}
}
