package bridge

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRouterListMethodsTracksLateRegistration(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})
	query := func(id string) Envelope {
		t.Helper()
		if err := r.Dispatch(`{"op":"list_methods","id":"` + id + `"}`); err != nil {
			t.Fatalf("dispatch list methods: %v", err)
		}
		return s.frameAt(s.len() - 1)
	}
	methods := func(env Envelope) []string {
		t.Helper()
		if env.Op != OpResponse {
			t.Fatalf("response = %+v", env)
		}
		var payload struct {
			Methods []string `json:"methods"`
		}
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			t.Fatalf("decode methods: %v", err)
		}
		return payload.Methods
	}
	if got := methods(query("empty")); len(got) != 0 {
		t.Fatalf("empty methods = %v", got)
	}
	if err := r.Register("z", func(ctx *Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("a", func(ctx *Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got, want := methods(query("late")), []string{"a", "z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("methods = %v, want %v", got, want)
	}
	if err := r.Register("a", func(ctx *Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got, want := methods(query("replace")), []string{"a", "z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("replacement methods = %v, want %v", got, want)
	}
	r.Unregister("z")
	if got, want := methods(query("unregister")), []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unregistered methods = %v, want %v", got, want)
	}
}

func TestRouterListMethodsRequiresIDAndKeepsRegistryDispatch(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})
	if err := r.Register("ordinary", func(ctx *Context) error { return ctx.Respond("ok") }); err != nil {
		t.Fatal(err)
	}
	if err := r.Dispatch(`{"op":"list_methods"}`); err != nil {
		t.Fatal(err)
	}
	if got := s.frameAt(0); got.Op != OpError || got.Error == nil || got.Error.Code != CodeDecode {
		t.Fatalf("missing-id response = %+v", got)
	}
	if err := r.Dispatch(`{"op":"req","id":"call","method":"ordinary"}`); err != nil {
		t.Fatal(err)
	}
	if got := s.frameAt(1); got.Op != OpResponse || string(got.Payload) != `"ok"` {
		t.Fatalf("ordinary response = %+v", got)
	}
	if got := r.MethodNames(); len(got) != 1 || got[0] != "ordinary" {
		t.Fatalf("registry methods = %v; query must not be registered", got)
	}
}
