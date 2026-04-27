package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/odvcencio/gosx/desktop/bridge"
)

type desktopTestService struct {
	seen string
}

type desktopTestPayload struct {
	Message string `json:"message"`
}

func (s *desktopTestService) Echo(payload desktopTestPayload) (desktopTestPayload, error) {
	return desktopTestPayload{Message: payload.Message + "!"}, nil
}

func (s *desktopTestService) Ping() string {
	return "pong"
}

func (s *desktopTestService) Save(ctx context.Context, payload *desktopTestPayload) error {
	if ctx == nil {
		return errors.New("missing context")
	}
	if payload == nil {
		return errors.New("missing payload")
	}
	s.seen = payload.Message
	return nil
}

func (s *desktopTestService) URLFor() string {
	return "app://gosx/static/"
}

func (s *desktopTestService) Fails() error {
	return errors.New("nope")
}

func (s *desktopTestService) Unsupported(first, second string) string {
	return first + second
}

func TestBindServiceDispatchesMethods(t *testing.T) {
	app, sent := newServiceTestApp()
	service := &desktopTestService{}
	binding, err := app.Bind("prefs", service)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if binding.Name != "prefs" {
		t.Fatalf("binding name = %q, want prefs", binding.Name)
	}
	if got := serviceMethodNames(binding); strings.Join(got, ",") != "echo,fails,ping,save,urlFor" {
		t.Fatalf("methods = %v", got)
	}

	dispatchServiceRequest(t, app, `{"op":"req","id":"1","method":"gosx.desktop.service.prefs.echo","payload":{"message":"hi"}}`)
	env := lastEnvelope(t, sent)
	if env.Op != bridge.OpResponse {
		t.Fatalf("op = %s, want response", env.Op)
	}
	var reply desktopTestPayload
	if err := json.Unmarshal(env.Payload, &reply); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if reply.Message != "hi!" {
		t.Fatalf("reply = %q, want hi!", reply.Message)
	}

	dispatchServiceRequest(t, app, `{"op":"req","id":"2","method":"gosx.desktop.service.prefs.save","payload":{"message":"stored"}}`)
	if service.seen != "stored" {
		t.Fatalf("service saw %q, want stored", service.seen)
	}
}

func TestBindServiceListAndUnbind(t *testing.T) {
	app, sent := newServiceTestApp()
	if _, err := app.Bind("prefs", &desktopTestService{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	dispatchServiceRequest(t, app, `{"op":"req","id":"list","method":"gosx.desktop.services.list"}`)
	env := lastEnvelope(t, sent)
	var bindings []ServiceBinding
	if err := json.Unmarshal(env.Payload, &bindings); err != nil {
		t.Fatalf("decode bindings: %v", err)
	}
	if len(bindings) != 1 || bindings[0].Name != "prefs" {
		t.Fatalf("bindings = %+v, want prefs", bindings)
	}

	if err := app.Unbind("prefs"); err != nil {
		t.Fatalf("Unbind: %v", err)
	}
	dispatchServiceRequest(t, app, `{"op":"req","id":"3","method":"gosx.desktop.service.prefs.ping"}`)
	env = lastEnvelope(t, sent)
	if env.Op != bridge.OpError || env.Error == nil || env.Error.Code != bridge.CodeNotFound {
		t.Fatalf("env = %+v, want not_found", env)
	}
}

func TestBindServiceValidation(t *testing.T) {
	app, _ := newServiceTestApp()
	if _, err := app.Bind("bad name", &desktopTestService{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("invalid name err = %v, want ErrInvalidOptions", err)
	}
	if _, err := app.Bind("empty", struct{}{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("empty service err = %v, want ErrInvalidOptions", err)
	}
	if _, err := app.Bind("nil", (*desktopTestService)(nil)); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("nil service err = %v, want ErrInvalidOptions", err)
	}
}

func TestBindServiceReturnsMethodErrors(t *testing.T) {
	app, sent := newServiceTestApp()
	if _, err := app.Bind("prefs", &desktopTestService{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	dispatchServiceRequest(t, app, `{"op":"req","id":"4","method":"gosx.desktop.service.prefs.fails"}`)
	env := lastEnvelope(t, sent)
	if env.Op != bridge.OpError || env.Error == nil || !strings.Contains(env.Error.Detail, "nope") {
		t.Fatalf("env = %+v, want error detail", env)
	}
}

func newServiceTestApp() (*App, *[]string) {
	sent := []string{}
	app := &App{}
	app.bridge = bridge.NewRouter(func(raw string) error {
		sent = append(sent, raw)
		return nil
	}, bridge.Limit{})
	app.registerServiceBridgeMethods()
	return app, &sent
}

func dispatchServiceRequest(t *testing.T, app *App, raw string) {
	t.Helper()
	if err := app.bridge.Dispatch(raw); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
}

func lastEnvelope(t *testing.T, sent *[]string) bridge.Envelope {
	t.Helper()
	if len(*sent) == 0 {
		t.Fatal("no envelopes sent")
	}
	env, err := bridge.ParseEnvelope((*sent)[len(*sent)-1])
	if err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	return env
}

func serviceMethodNames(binding ServiceBinding) []string {
	names := make([]string, len(binding.Methods))
	for i, method := range binding.Methods {
		names[i] = method.Name
	}
	return names
}
