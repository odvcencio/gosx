package desktop

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildMenuPlanAssignsCommandIDs(t *testing.T) {
	plan, err := BuildMenuPlan(Menu{Items: []MenuItem{
		{Label: "File", Submenu: &Menu{Items: []MenuItem{
			{ID: "open", Label: "Open"},
			{Separator: true},
			{ID: "quit", Label: "Quit", Disabled: true},
		}}},
		{ID: "help", Label: "Help", Checked: true},
	}})
	if err != nil {
		t.Fatalf("build menu: %v", err)
	}
	if plan.NextCommandID != firstNativeCommandID+3 {
		t.Fatalf("next command id = %d", plan.NextCommandID)
	}
	file := plan.Items[0]
	if file.Label != "File" || len(file.Items) != 3 {
		t.Fatalf("file menu = %+v", file)
	}
	if got := file.Items[0].CommandID; got != firstNativeCommandID {
		t.Fatalf("open command id = %d", got)
	}
	if !file.Items[1].Separator {
		t.Fatalf("second file item = %+v, want separator", file.Items[1])
	}
	if got := plan.Items[1].CommandID; got != firstNativeCommandID+2 {
		t.Fatalf("help command id = %d", got)
	}
	if !plan.Items[1].Checked || !file.Items[2].Disabled {
		t.Fatalf("menu flags not preserved: %+v %+v", plan.Items[1], file.Items[2])
	}
}

func TestBuildMenuPlanRejectsInvalidItems(t *testing.T) {
	_, err := BuildMenuPlan(Menu{Items: []MenuItem{{Label: "Bad\x00Label"}}})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("err = %v, want ErrInvalidOptions", err)
	}
	_, err = BuildMenuPlan(Menu{Items: []MenuItem{{Submenu: &Menu{}}}})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("empty submenu err = %v, want ErrInvalidOptions", err)
	}
}

func TestBuildTrayRegistration(t *testing.T) {
	reg, err := BuildTrayRegistration("com.example.gosx", TrayOptions{
		Icon:    `C:\Apps\gosx.ico`,
		Tooltip: "GoSX Desktop",
		Menu:    Menu{Items: []MenuItem{{ID: "show", Label: "Show"}}},
	})
	if err != nil {
		t.Fatalf("build tray: %v", err)
	}
	if reg.AppID != "com.example.gosx" || reg.Icon != `C:\Apps\gosx.ico` {
		t.Fatalf("tray registration = %+v", reg)
	}
	if got := reg.Menu.Items[0].CommandID; got != firstNativeCommandID {
		t.Fatalf("tray menu command id = %d", got)
	}
}

func TestBuildTrayRegistrationRejectsLongTooltip(t *testing.T) {
	_, err := BuildTrayRegistration("com.example.gosx", TrayOptions{
		Tooltip: strings.Repeat("x", maxTrayTooltipRunes+1),
	})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("err = %v, want ErrInvalidOptions", err)
	}
}

func TestBuildToastPayload(t *testing.T) {
	payload, err := BuildToastPayload(Notification{
		Title:  `Build <ready>`,
		Body:   `Assets & shaders updated`,
		Silent: true,
		Actions: []NotificationAction{
			{ID: "open", Label: "Open"},
		},
	})
	if err != nil {
		t.Fatalf("build toast: %v", err)
	}
	for _, want := range []string{
		`<toast launch="gosx-notification">`,
		`Build &lt;ready&gt;`,
		`Assets &amp; shaders updated`,
		`<action activationType="foreground" content="Open" arguments="open"/>`,
		`<audio silent="true"/>`,
	} {
		if !strings.Contains(payload.XML, want) {
			t.Fatalf("toast XML missing %q:\n%s", want, payload.XML)
		}
	}
	if len(payload.ActionIDs) != 1 || payload.ActionIDs[0] != "open" {
		t.Fatalf("action ids = %#v", payload.ActionIDs)
	}
}

func TestBuildToastPayloadRejectsInvalidInput(t *testing.T) {
	_, err := BuildToastPayload(Notification{})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("empty title err = %v, want ErrInvalidOptions", err)
	}
	_, err = BuildToastPayload(Notification{
		Title: "ok",
		Actions: []NotificationAction{
			{ID: "ok", Label: "OK"},
			{ID: "2", Label: "Two"},
			{ID: "3", Label: "Three"},
			{ID: "4", Label: "Four"},
			{ID: "5", Label: "Five"},
			{ID: "6", Label: "Six"},
		},
	})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("too many actions err = %v, want ErrInvalidOptions", err)
	}
}

func TestDispatchFileDropCopiesPaths(t *testing.T) {
	paths := []string{`C:\tmp\a.gsx`, `C:\tmp\b.gsx`}
	var got []string
	dispatchFileDrop(func(in []string) {
		got = in
		in[0] = "mutated"
	}, paths)
	if paths[0] != `C:\tmp\a.gsx` {
		t.Fatalf("source paths mutated: %#v", paths)
	}
	if got[0] != "mutated" || got[1] != `C:\tmp\b.gsx` {
		t.Fatalf("callback paths = %#v", got)
	}
}

func TestNormalizeDPIAwarenessDefault(t *testing.T) {
	options, err := normalizeOptions(Options{})
	if err != nil {
		t.Fatalf("normalize options: %v", err)
	}
	if options.DPIAwareness != DPIAwarenessPerMonitorV2 {
		t.Fatalf("dpi awareness = %q", options.DPIAwareness)
	}
	_, err = normalizeOptions(Options{DPIAwareness: "bad"})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("bad dpi err = %v, want ErrInvalidOptions", err)
	}
}
