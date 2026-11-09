package kitui

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/boot"
	"m31labs.dev/gosx/game/settings"
)

func has(t *testing.T, html string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(html, s) {
			t.Errorf("missing %q in\n%s", s, html)
		}
	}
}

func TestPauseMenu(t *testing.T) {
	html := gosx.RenderHTML(PauseMenu(PauseProps{}))
	has(t, html, `role="dialog"`, `aria-modal="true"`, `aria-labelledby="kit-pause-title"`, `id="kit-pause-title"`,
		`data-kit-action="resume"`, `data-kit-action="settings"`, `data-kit-action="quit"`, `>Paused<`, `autofocus`, `data-kit-primary`)
	if strings.Contains(html, "hidden") {
		t.Error("should be visible by default")
	}
	html = gosx.RenderHTML(PauseMenu(PauseProps{Title: "<b>x</b>", Hidden: true, Items: []MenuItem{{ID: "go", Label: "Go", Disabled: true}}}))
	has(t, html, ` hidden`, `data-kit-action="go"`, `disabled`, `&lt;b&gt;`)
}

func TestSettingsPanelAndApply(t *testing.T) {
	m := settings.NewManager(settings.Config{})
	m.Update(func(s *settings.Settings) { s.Volume.Music = 0.4; s.Quality = settings.QualityHigh; s.FrameCap = 60 })
	html := gosx.RenderHTML(SettingsPanel(SettingsProps{Settings: m.Get(), Languages: []Option{{"en", "English"}, {"de", "Deutsch"}}}))
	for _, bus := range settings.Buses {
		has(t, html, `data-kit-setting="volume.`+bus+`"`)
	}
	has(t, html, `data-kit-setting="renderScale"`, `data-kit-setting="frameCap"`, `data-kit-setting="quality"`,
		`data-kit-setting="motion"`, `data-kit-setting="cameraShake"`, `data-kit-setting="language"`,
		`value="40"`, `<option value="high" selected`, `<option value="60" selected`, `type="range"`, `Deutsch`,
		`data-kit-action="settings-close"`, `data-kit-action="settings-reset"`)
	if strings.Contains(gosx.RenderHTML(SettingsPanel(SettingsProps{Settings: m.Get(), NoAudio: true})), "volume.music") {
		t.Error("NoAudio ignored")
	}
	// Labels can be replaced.
	has(t, gosx.RenderHTML(SettingsPanel(SettingsProps{Settings: m.Get(), Labels: map[string]string{"title": "Optionen"}})), "Optionen")

	for _, c := range []struct{ k, v string }{
		{"volume.sfx", "30"}, {"renderScale", "75"}, {"cameraShake", "50"}, {"frameCap", "120"},
		{"quality", "low"}, {"motion", "reduce"}, {"language", "en"},
	} {
		if _, err := ApplySetting(m, c.k, c.v); err != nil {
			t.Errorf("%s: %v", c.k, err)
		}
	}
	s := m.Get()
	if s.Volume.SFX != 0.3 || s.RenderScale != 0.75 || s.CameraShake != 0.5 || s.FrameCap != 120 || s.Quality != settings.QualityLow || s.Motion != settings.MotionReduce {
		t.Fatalf("%+v", s)
	}
	for _, bad := range []struct{ k, v string }{{"volume.bogus", "1"}, {"volume.sfx", "loud"}, {"nope", "1"}, {"frameCap", "x"}} {
		if _, err := ApplySetting(m, bad.k, bad.v); err == nil {
			t.Errorf("%v should fail", bad)
		}
	}
	if m.Get().Volume != s.Volume || m.Get().FrameCap != s.FrameCap {
		t.Error("failed apply changed state")
	}
	// Out-of-range input is clamped by the manager.
	ApplySetting(m, "volume.master", "900")
	if m.Get().Volume.Master != 1 {
		t.Error("clamp")
	}
}

func TestRebindPanel(t *testing.T) {
	am := actions.NewMap()
	am.Define(actions.Context{Name: "gameplay", Actions: []actions.ActionDef{
		{Name: "jump", Bindings: []actions.Binding{{Source: actions.Key("Space")}, {Source: actions.Key("KeyJ")}, {Source: actions.PadButton(0)}}},
		{Name: "fire", Bindings: []actions.Binding{{Source: actions.MouseButton(0)}, {}}},
	}})
	html := gosx.RenderHTML(RebindPanel(RebindProps{Map: am, Context: "gameplay", Names: map[string]string{"jump": "Jump"}, Message: "Space is used by Fire"}))
	has(t, html, `data-kit-rebind="gameplay/jump/0"`, `data-kit-rebind="gameplay/jump/1"`, `>Space<`, `>J<`, `Left Click`, `>Jump<`,
		`>fire<`, `Unbound`, `role="status"`, `Space is used by Fire`, `aria-pressed="false"`)
	if strings.Contains(html, "gameplay/jump/2") {
		t.Error("gamepad slot shown on the keyboard tab")
	}
	pad := gosx.RenderHTML(RebindPanel(RebindProps{Map: am, Context: "gameplay", Class: actions.ClassGamepad, Style: actions.PadXbox}))
	has(t, pad, `data-kit-rebind="gameplay/jump/2"`, `>A<`)
	listening := gosx.RenderHTML(RebindPanel(RebindProps{Map: am, Context: "gameplay", Listening: true, ListeningAction: "jump", ListeningSlot: 1}))
	has(t, listening, `aria-pressed="true"`, "Press a key...")
	ctx, action, slot, ok := ParseRebindID(RebindID("gameplay", "jump", 1))
	if !ok || ctx != "gameplay" || action != "jump" || slot != 1 {
		t.Fatal(ctx, action, slot, ok)
	}
	for _, bad := range []string{"", "a/b", "a/b/x", "/b/1", "a/b/-1"} {
		if _, _, _, ok := ParseRebindID(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestBootRoot(t *testing.T) {
	html := gosx.RenderHTML(BootRoot(LoadingProps{}, gosx.El("canvas")))
	has(t, html, `data-boot-root`, `data-boot="loading"`, `aria-busy="true"`, `--boot-progress:0.000`, `role="progressbar"`,
		`data-boot-title`, `data-boot-copy`, `data-boot-retry`, `<canvas`)
	failed := gosx.RenderHTML(BootRoot(LoadingProps{Snapshot: boot.Snapshot{Phase: boot.PhaseFailed, Progress: 0.5}}))
	has(t, failed, `data-boot="failed"`, `--boot-progress:0.500`, `aria-valuenow="50"`, "did not finish loading")
	ready := gosx.RenderHTML(BootRoot(LoadingProps{Snapshot: boot.Snapshot{Phase: boot.PhaseReady, Progress: 1}}))
	has(t, ready, `data-boot="ready"`, `aria-busy="false"`)
}

func TestAssets(t *testing.T) {
	for _, v := range []string{"--kit-accent", "--kit-bg", "--kit-fg", "--kit-panel-radius", "--kit-focus"} {
		if !strings.Contains(CSS(), v) {
			t.Errorf("css variable %s missing", v)
		}
	}
	if !strings.Contains(gosx.RenderHTML(Styles()), "<style") || !strings.Contains(gosx.RenderHTML(Script()), "gosx:kit") {
		t.Error("style or script node")
	}
	if strings.Contains(ScriptSource(), "</script") {
		t.Error("script closes itself")
	}
}
