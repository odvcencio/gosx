package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// sink records outbound envelopes so tests can assert on them.
type sink struct {
	mu     sync.Mutex
	frames []Envelope
}

func (s *sink) send(raw string) error {
	var env Envelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return fmt.Errorf("sink unmarshal: %w", err)
	}
	s.mu.Lock()
	s.frames = append(s.frames, env)
	s.mu.Unlock()
	return nil
}

func (s *sink) frameAt(i int) Envelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i >= len(s.frames) {
		return Envelope{}
	}
	return s.frames[i]
}

func (s *sink) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.frames)
}

func TestRouterDispatchRequestRespond(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})

	r.Register("echo", func(ctx *Context) error {
		var req struct {
			Word string `json:"word"`
		}
		if err := ctx.Decode(&req); err != nil {
			return err
		}
		return ctx.Respond(map[string]string{"echo": req.Word})
	})

	raw := `{"op":"req","id":"1","method":"echo","payload":{"word":"hi"}}`
	if err := r.Dispatch(raw); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if s.len() != 1 {
		t.Fatalf("frames = %d, want 1", s.len())
	}
	got := s.frameAt(0)
	if got.Op != OpResponse || got.ID != "1" {
		t.Fatalf("frame = %+v", got)
	}
	if !strings.Contains(string(got.Payload), `"echo":"hi"`) {
		t.Fatalf("payload = %s", got.Payload)
	}
}

func TestRouterVoidHandlerAutoResponds(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})
	r.Register("tick", func(ctx *Context) error { return nil })

	if err := r.Dispatch(`{"op":"req","id":"9","method":"tick"}`); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := s.frameAt(0)
	if got.Op != OpResponse || got.ID != "9" {
		t.Fatalf("frame = %+v", got)
	}
}

func TestRouterHandlerErrorSurfacesAsOpError(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})
	r.Register("oops", func(ctx *Context) error { return errors.New("boom") })

	_ = r.Dispatch(`{"op":"req","id":"2","method":"oops"}`)
	got := s.frameAt(0)
	if got.Op != OpError || got.ID != "2" {
		t.Fatalf("frame = %+v", got)
	}
	if got.Error == nil || got.Error.Code != CodeInternal {
		t.Fatalf("error = %+v", got.Error)
	}
	if !strings.Contains(got.Error.Detail, "boom") {
		t.Fatalf("detail = %q", got.Error.Detail)
	}
}

func TestRouterHandlerPanicSurfacesAsOpError(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})
	r.Register("explode", func(ctx *Context) error {
		panic("segfault-simulator")
	})

	_ = r.Dispatch(`{"op":"req","id":"3","method":"explode"}`)
	got := s.frameAt(0)
	if got.Op != OpError || got.ID != "3" {
		t.Fatalf("frame = %+v", got)
	}
	if !strings.Contains(got.Error.Detail, "segfault-simulator") {
		t.Fatalf("detail missing panic value: %q", got.Error.Detail)
	}
}

func TestRouterExplicitError(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})
	r.Register("validate", func(ctx *Context) error {
		return ctx.Error("validate.missing_field", "payload lacks required field")
	})

	_ = r.Dispatch(`{"op":"req","id":"4","method":"validate"}`)
	got := s.frameAt(0)
	if got.Error == nil || got.Error.Code != "validate.missing_field" {
		t.Fatalf("error = %+v", got.Error)
	}
}

func TestRouterStreamingExchange(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})
	r.Register("count", func(ctx *Context) error {
		for i := 0; i < 3; i++ {
			if err := ctx.Stream(map[string]int{"n": i}); err != nil {
				return err
			}
		}
		return ctx.End()
	})

	_ = r.Dispatch(`{"op":"req","id":"s1","method":"count"}`)
	if s.len() != 4 {
		t.Fatalf("frames = %d, want 3 stream frames + 1 end", s.len())
	}
	for i := 0; i < 3; i++ {
		f := s.frameAt(i)
		if f.Op != OpStreamFrame || f.ID != "s1" {
			t.Fatalf("frame %d = %+v", i, f)
		}
	}
	last := s.frameAt(3)
	if last.Op != OpStreamEnd || last.ID != "s1" {
		t.Fatalf("terminator = %+v", last)
	}
}

func TestRouterDoubleRespondIsNoOp(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})
	r.Register("twice", func(ctx *Context) error {
		_ = ctx.Respond("first")
		_ = ctx.Respond("second")
		_ = ctx.Error("x", "y")
		return nil
	})

	_ = r.Dispatch(`{"op":"req","id":"d","method":"twice"}`)
	if s.len() != 1 {
		t.Fatalf("frames = %d, want 1", s.len())
	}
	if !strings.Contains(string(s.frameAt(0).Payload), "first") {
		t.Fatalf("first respond didn't win: %s", s.frameAt(0).Payload)
	}
}

func TestRouterMethodNotFound(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})

	_ = r.Dispatch(`{"op":"req","id":"5","method":"ghost"}`)
	got := s.frameAt(0)
	if got.Error == nil || got.Error.Code != CodeNotFound {
		t.Fatalf("error = %+v", got.Error)
	}
}

func TestRouterRejectsRequestWithoutID(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})

	_ = r.Dispatch(`{"op":"req","method":"echo"}`)
	got := s.frameAt(0)
	if got.Error == nil || got.Error.Code != CodeDecode {
		t.Fatalf("error = %+v", got.Error)
	}
}

func TestRouterRejectsRequestWithoutMethod(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})

	_ = r.Dispatch(`{"op":"req","id":"6"}`)
	got := s.frameAt(0)
	if got.Error == nil || got.Error.Code != CodeDecode {
		t.Fatalf("error = %+v", got.Error)
	}
}

func TestRouterIgnoresHostOnlyOps(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})

	for _, op := range []string{"res", "err", "frame", "end"} {
		raw := fmt.Sprintf(`{"op":%q,"id":"x"}`, op)
		if err := r.Dispatch(raw); err != nil {
			t.Fatalf("dispatch %s: %v", op, err)
		}
	}
	if s.len() != 0 {
		t.Fatalf("expected 0 outbound frames, got %d", s.len())
	}
}

func TestRouterRejectsUnknownOp(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})

	_ = r.Dispatch(`{"op":"ransack","id":"z"}`)
	got := s.frameAt(0)
	if got.Error == nil || got.Error.Code != CodeInvalidOp {
		t.Fatalf("error = %+v", got.Error)
	}
}

func TestRouterRejectsOversizedPayload(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{MaxBytes: 64})

	big := strings.Repeat("x", 100)
	raw := fmt.Sprintf(`{"op":"req","id":"1","method":"a","payload":%q}`, big)
	_ = r.Dispatch(raw)
	got := s.frameAt(0)
	if got.Error == nil || got.Error.Code != CodeTooLarge {
		t.Fatalf("error = %+v", got.Error)
	}
}

func TestRouterRateLimitRejectsBurst(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{Rate: 1, Burst: 2})

	raw := `{"op":"req","id":"1","method":"x"}`
	_ = r.Dispatch(raw) // token 1 -> not_found
	_ = r.Dispatch(raw) // token 2 -> not_found
	_ = r.Dispatch(raw) // no tokens -> rate_limited

	f3 := s.frameAt(2)
	if f3.Error == nil || f3.Error.Code != CodeRateLimited {
		t.Fatalf("third frame = %+v", f3.Error)
	}
}

func TestRouterRateLimitRecovers(t *testing.T) {
	s := &sink{}
	// Very high rate so one sleep tick restores capacity.
	r := NewRouter(s.send, Limit{Rate: 1000, Burst: 1})

	raw := `{"op":"req","id":"1","method":"x"}`
	_ = r.Dispatch(raw)
	time.Sleep(5 * time.Millisecond) // should refill many tokens
	_ = r.Dispatch(raw)

	// Both should surface CodeNotFound (method unregistered), neither
	// CodeRateLimited.
	for i := 0; i < 2; i++ {
		f := s.frameAt(i)
		if f.Error == nil || f.Error.Code == CodeRateLimited {
			t.Fatalf("frame %d was rate-limited unexpectedly: %+v", i, f.Error)
		}
	}
}

func TestRouterDisabledRateLimit(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{MaxBytes: 64 * 1024})

	for i := 0; i < 50; i++ {
		_ = r.Dispatch(`{"op":"req","id":"1","method":"x"}`)
	}
	for i := 0; i < 50; i++ {
		if s.frameAt(i).Error.Code == CodeRateLimited {
			t.Fatalf("frame %d rate-limited with disabled Rate", i)
		}
	}
}

func TestRouterEmitEvent(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})
	if err := r.Emit("toast.show", map[string]string{"msg": "saved"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	got := s.frameAt(0)
	if got.Op != OpEvent || got.Method != "toast.show" {
		t.Fatalf("frame = %+v", got)
	}
}

func TestRouterUnregister(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})
	r.Register("hot", func(ctx *Context) error { return ctx.Respond("ok") })
	r.Unregister("hot")

	_ = r.Dispatch(`{"op":"req","id":"1","method":"hot"}`)
	got := s.frameAt(0)
	if got.Error == nil || got.Error.Code != CodeNotFound {
		t.Fatalf("error = %+v", got.Error)
	}
}

func TestRouterRegisterEmptyNamePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty method name")
		}
	}()
	r := NewRouter(func(string) error { return nil }, Limit{})
	r.Register("", func(*Context) error { return nil })
}

func TestLimitWithDefaults(t *testing.T) {
	zero := Limit{}.withDefaults()
	if zero != DefaultLimit {
		t.Fatalf("zero Limit defaults = %+v, want %+v", zero, DefaultLimit)
	}

	l := Limit{Rate: 100}
	got := l.withDefaults()
	if got.Burst != 100 {
		t.Fatalf("Burst = %d, want 100", got.Burst)
	}

	explicit := Limit{Rate: 100, Burst: 400}.withDefaults()
	if explicit.Burst != 400 {
		t.Fatalf("explicit Burst overwritten: %d", explicit.Burst)
	}
}

func TestRouterInvalidJSON(t *testing.T) {
	s := &sink{}
	r := NewRouter(s.send, Limit{})
	_ = r.Dispatch(`not-json`)
	got := s.frameAt(0)
	if got.Error == nil || got.Error.Code != CodeDecode {
		t.Fatalf("error = %+v", got.Error)
	}
}
