package desktop

import (
	"encoding/json"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/desktop/bridge"
)

type capabilityPlatformApp struct {
	recordingPlatformApp
	script string
	err    error
}

func (a *capabilityPlatformApp) PrependBootstrapScript(script string) error {
	if a.err != nil {
		return a.err
	}
	a.script = script
	return nil
}

func TestServiceCapabilityPublicationFailurePreservesBinding(t *testing.T) {
	for _, operation := range []string{"bind", "replace", "unbind"} {
		t.Run(operation, func(t *testing.T) {
			app, sent := newServiceTestApp()
			platform := &capabilityPlatformApp{}
			app.impl = platform
			if _, err := app.Bind("diagnostics", &partialDiagnostics{}); err != nil {
				t.Fatal(err)
			}
			beforeBindings, beforeMethods := app.ServiceBindings(), app.Bridge().MethodNames()
			beforePreload := append([]string(nil), app.preloadScripts...)
			beforeRevision, beforeSent := app.methodRevision, len(*sent)
			platform.err = errors.New("injected preload failure")
			var err error
			switch operation {
			case "bind":
				_, err = app.Bind("newService", &desktopTestService{})
			case "replace":
				_, err = app.Bind("diagnostics", &desktopTestService{})
			case "unbind":
				err = app.Unbind("diagnostics")
			}
			if !errors.Is(err, platform.err) {
				t.Fatalf("publication error = %v", err)
			}
			if !reflect.DeepEqual(app.ServiceBindings(), beforeBindings) || !reflect.DeepEqual(app.Bridge().MethodNames(), beforeMethods) ||
				!reflect.DeepEqual(app.preloadScripts, beforePreload) || app.methodRevision != beforeRevision || len(*sent) != beforeSent || platform.script != strings.Join(beforePreload, "\n;\n") {
				t.Fatal("failed publication changed binding, router, preload, revision or live metadata")
			}
			dispatchServiceRequest(t, app, `{"op":"req","id":"still-bound","method":"gosx.desktop.service.diagnostics.preview"}`)
			if string(lastEnvelope(t, sent).Payload) != `"preview"` {
				t.Fatal("previous service handler was lost")
			}
		})
	}
}

func TestServiceCapabilityPreloadStateIsBounded(t *testing.T) {
	app, _ := newServiceTestApp()
	platform := &capabilityPlatformApp{}
	app.impl = platform
	if err := app.refreshBridgeCapabilities(); err != nil {
		t.Fatal(err)
	}
	if err := app.addPreloadScript("window.appPreload = true;"); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if _, err := app.Bind("diagnostics", &partialDiagnostics{}); err != nil {
			t.Fatal(err)
		}
		if err := app.Unbind("diagnostics"); err != nil {
			t.Fatal(err)
		}
		if len(app.preloadScripts) != 2 || strings.Count(platform.script, "var registeredMethods = new Set()") != 1 || strings.Count(platform.script, "window.appPreload = true;") != 1 {
			t.Fatal("repeated service updates retained additional scripts")
		}
	}
}

type capabilityUIPlatform struct {
	capabilityPlatformApp
	app     *App
	t       *testing.T
	updates int
}

func (p *capabilityUIPlatform) onUIThread(update func() error) error {
	if !p.app.serviceUpdateMu.TryLock() {
		p.t.Fatal("service update dispatched while holding service lock")
	}
	p.app.serviceUpdateMu.Unlock()
	if !p.app.preloadMu.TryLock() {
		p.t.Fatal("service update dispatched while holding preload lock")
	}
	p.app.preloadMu.Unlock()
	p.updates++
	return update()
}

func TestServiceCapabilitiesDispatchBeforeLocking(t *testing.T) {
	app, _ := newServiceTestApp()
	platform := &capabilityUIPlatform{app: app, t: t}
	app.impl = platform
	if _, err := app.Bind("diagnostics", &partialDiagnostics{}); err != nil {
		t.Fatal(err)
	}
	if err := app.Unbind("diagnostics"); err != nil {
		t.Fatal(err)
	}
	if platform.updates != 2 {
		t.Fatalf("dispatched %d service mutations, want 2", platform.updates)
	}
}

type partialDiagnostics struct{}

func (*partialDiagnostics) Preview() string { return "preview" }

func TestBridgeCapabilityPublication(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable")
	}
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "raw", true: "native"}[native], func(t *testing.T) {
			app, sent := newServiceTestApp()
			platform := &capabilityPlatformApp{}
			app.impl = platform
			app.options.NativeBridge = native
			if native {
				if err := app.registerNativeBridgeMethods(); err != nil {
					t.Fatal(err)
				}
			}
			if err := app.refreshBridgeCapabilities(); err != nil {
				t.Fatal(err)
			}
			initial := platform.script
			if _, err := app.Bind("diagnostics", &partialDiagnostics{}); err != nil {
				t.Fatal(err)
			}
			bound := platform.script
			boundEvent := (*sent)[len(*sent)-1]
			if err := app.addPreloadScript(`window.afterBridge = window.gosxDesktop.hasMethod("gosx.desktop.service.diagnostics.preview");`); err != nil {
				t.Fatal(err)
			}
			withAppPreload := platform.script
			if err := app.Unbind("diagnostics"); err != nil {
				t.Fatal(err)
			}
			unbound := platform.script
			unboundEvent := (*sent)[len(*sent)-1]
			// Metadata does not gate direct router handlers or raw calls.
			if err := app.Bridge().Register("legacy.echo", func(ctx *bridge.Context) error { return ctx.Respond("raw-ok") }); err != nil {
				t.Fatal(err)
			}
			dispatchServiceRequest(t, app, `{"op":"req","id":"raw","method":"legacy.echo"}`)
			if string(lastEnvelope(t, sent).Payload) != `"raw-ok"` {
				t.Fatal("raw registration lifecycle changed")
			}
			data, _ := json.Marshal(map[string]any{
				"initial": initial, "bound": bound, "unbound": unbound, "native": native,
				"boundEvent": boundEvent, "unboundEvent": unboundEvent, "withAppPreload": withAppPreload,
			})
			cmd := exec.Command(node, "testdata/capabilities.cjs")
			cmd.Stdin = strings.NewReader(string(data))
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("capabilities: %v\n%s", err, output)
			}
		})
	}
}
