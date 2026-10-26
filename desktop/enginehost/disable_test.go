package enginehost

import (
	"testing"

	"m31labs.dev/gosx/desktop/bridge"
	"m31labs.dev/gosx/engine/host"
)

func TestAttachHonorsDisableNative(t *testing.T) {
	for _, tc := range []struct {
		name         string
		disable      []string
		component    string
		wantDisabled bool
	}{
		{name: "nil enables all", component: "Sim"},
		{name: "empty enables all", disable: []string{}, component: "Sim"},
		{name: "empty trimmed entries ignored", disable: []string{"  "}, component: "Sim"},
		{name: "exact match", disable: []string{"Sim"}, component: "Sim", wantDisabled: true},
		{name: "trim whitespace", disable: []string{" Sim "}, component: "Sim", wantDisabled: true},
		{name: "non-match", disable: []string{"Other"}, component: "Sim"},
		{name: "case-sensitive", disable: []string{"sim"}, component: "Sim"},
		{name: "wildcard", disable: []string{"*"}, component: "Sim", wantDisabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newFakePage(t)
			h := New(p.router, Options{DisableNative: tc.disable})
			t.Cleanup(h.Close)
			if err := h.Register("Sim", func(*Instance) (host.Handle, error) {
				return host.HandleFunc(func() {}), nil
			}); err != nil {
				t.Fatal(err)
			}
			env := p.attach("e1", tc.component, false)
			if tc.wantDisabled {
				if env.Op != bridge.OpError || env.Error.Code != CodeNativeDisabled {
					t.Fatalf("attach = %+v; want %s", env, CodeNativeDisabled)
				}
			} else if env.Op != bridge.OpResponse {
				t.Fatalf("attach = %+v; want success", env)
			}
		})
	}
}

func TestDisableNativeDoesNotCreateSessionOrCallFactory(t *testing.T) {
	p := newFakePage(t)
	disabled := []string{"Sim"}
	h := New(p.router, Options{DisableNative: disabled})
	t.Cleanup(h.Close)
	factoryCalls := 0
	for _, component := range []string{"Allowed", "Sim"} {
		if err := h.Register(component, func(*Instance) (host.Handle, error) {
			factoryCalls++
			return host.HandleFunc(func() {}), nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	disabled[0] = "Other"
	if got := p.attach("unattached", "Sim", false); got.Op != bridge.OpError || got.Error.Code != CodeNativeDisabled {
		t.Fatalf("disabled attach = %+v", got)
	}
	post := p.request(MethodPost, map[string]any{"id": "unattached", "msg": map[string]any{"t": "mount"}})
	if post.Op != bridge.OpError || post.Error.Code != CodeNotRegistered {
		t.Fatalf("post after disabled attach = %+v", post)
	}
	if got := p.attach("same", "Allowed", false); got.Op != bridge.OpResponse {
		t.Fatalf("initial attach = %+v", got)
	}
	if got := p.attach("same", "Sim", false); got.Op != bridge.OpError || got.Error.Code != CodeNativeDisabled {
		t.Fatalf("disabled replacement = %+v", got)
	}
	if factoryCalls != 0 {
		t.Fatalf("factory calls before mount = %d", factoryCalls)
	}
	p.post("same", map[string]any{"t": "mount"})
	p.waitMsg("same", "mounted", 1)
	if factoryCalls != 1 {
		t.Fatalf("existing session was replaced or disabled factory called: %d", factoryCalls)
	}
}
