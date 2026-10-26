//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	modCmd   = nsEventModifierFlagCommand
	modCtrl  = nsEventModifierFlagControl
	modOpt   = nsEventModifierFlagOption
	modShift = nsEventModifierFlagShift
)

func TestDarwinParseMenuAccelerator(t *testing.T) {
	for _, tc := range []struct {
		in   string
		key  string
		mask uintptr
		ok   bool
	}{
		// The chord formats SetMenuBar documents.
		{"cmd+N", "n", modCmd, true},
		{"cmd+shift+Z", "z", modCmd | modShift, true},
		{"cmd+,", ",", modCmd, true},
		{"CMD+SHIFT+Z", "z", modCmd | modShift, true},
		{"Cmd+Alt+I", "i", modCmd | modOpt, true},
		{"ctrl+alt+shift+cmd+k", "k", modCtrl | modOpt | modShift | modCmd, true},
		// Windows-style labels parse as written: Ctrl is Control.
		{"Ctrl+S", "s", modCtrl, true},
		{"Ctrl+Shift+S", "s", modCtrl | modShift, true},
		{" Ctrl + S ", "s", modCtrl, true},
		// Names for the same keys, and the labels written for every platform.
		{"Command+O", "o", modCmd, true},
		{"Meta+O", "o", modCmd, true},
		{"Option+O", "o", modOpt, true},
		{"Control+O", "o", modCtrl, true},
		{"CmdOrCtrl+P", "p", modCmd, true},
		{"Mod+P", "p", modCmd, true},
		// The key is the last element, and "+" can be the key.
		{"Cmd++", "+", modCmd, true},
		{"Cmd+Plus", "+", modCmd, true},
		{"cmd+1", "1", modCmd, true},
		{"cmd+é", "é", modCmd, true},
		// Named keys.
		{"Cmd+Enter", "\r", modCmd, true},
		{"Cmd+Return", "\r", modCmd, true},
		{"Ctrl+Tab", "\t", modCtrl, true},
		{"Cmd+Esc", "\x1b", modCmd, true},
		{"Cmd+Space", " ", modCmd, true},
		{"Cmd+Backspace", "\x08", modCmd, true},
		{"Cmd+Delete", "\uf728", modCmd, true},
		{"Cmd+Up", "\uf700", modCmd, true},
		{"Cmd+Down", "\uf701", modCmd, true},
		{"Cmd+Left", "\uf702", modCmd, true},
		{"Cmd+Right", "\uf703", modCmd, true},
		{"Cmd+Home", "\uf729", modCmd, true},
		{"Cmd+End", "\uf72b", modCmd, true},
		{"Cmd+PageUp", "\uf72c", modCmd, true},
		{"Cmd+PageDown", "\uf72d", modCmd, true},
		// Function keys may stand alone: they are not typed into a page.
		{"F5", "\uf708", 0, true},
		{"f1", "\uf704", 0, true},
		{"F12", "\uf70f", 0, true},
		{"F20", "\uf717", 0, true},
		{"Shift+F5", "\uf708", modShift, true},
		{"Cmd+F3", "\uf706", modCmd, true},
		{"Alt+F4", "\uf707", modOpt, true},
		// Not usable. A printable key alone, or with Shift alone, would take every
		// keystroke away from the page.
		{"", "", 0, false},
		{"   ", "", 0, false},
		{"N", "", 0, false},
		{"Shift+N", "", 0, false},
		{"Esc", "", 0, false},
		{"Enter", "", 0, false},
		{"Delete", "", 0, false},
		{"+", "", 0, false},
		{"cmd+", "", 0, false},
		{"cmd", "", 0, false},
		{"cmd+shift", "", 0, false},
		{"hyper+N", "", 0, false},
		{"cmd+nosuchkey", "", 0, false},
		{"cmd+F0", "", 0, false},
		{"cmd+F21", "", 0, false},
		{"cmd+F1x", "", 0, false},
		{"cmd+\x01", "", 0, false},
		{"cmd+\xff", "", 0, false},
		{"cmd+N+M", "", 0, false},
		{"++", "", 0, false},
	} {
		got, ok := parseMenuAccelerator(tc.in)
		if ok != tc.ok {
			t.Errorf("parseMenuAccelerator(%q) ok = %v, want %v (got %+v)", tc.in, ok, tc.ok, got)
			continue
		}
		if ok && (got.key != tc.key || got.mask != tc.mask) {
			t.Errorf("parseMenuAccelerator(%q) = {%q %#x}, want {%q %#x}", tc.in, got.key, got.mask, tc.key, tc.mask)
		}
	}
}

// The modifier flags are the NSEventModifierFlags values AppKit documents; a wrong
// bit would put a different key in the menu.
func TestDarwinMenuModifierFlagValues(t *testing.T) {
	for name, tc := range map[string]struct{ got, want uintptr }{
		"shift":   {nsEventModifierFlagShift, 0x20000},
		"control": {nsEventModifierFlagControl, 0x40000},
		"option":  {nsEventModifierFlagOption, 0x80000},
		"command": {nsEventModifierFlagCommand, 0x100000},
	} {
		if tc.got != tc.want {
			t.Errorf("%s flag = %#x, want %#x", name, tc.got, tc.want)
		}
	}
}

func TestDarwinMenuTitle(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"File", "File"},
		{"&File", "File"},
		{"E&xit", "Exit"},
		{"&Open...", "Open..."},
		{"Save && Close", "Save & Close"},
		{"&&", "&"},
		{"A&&&B", "A&B"},
		{"Trailing&", "Trailing"},
		{"  padded  ", "padded"},
		{"日本&語", "日本語"},
		{"", ""},
	} {
		if got := menuTitle(tc.in); got != tc.want {
			t.Errorf("menuTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDarwinApplicationName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Cicada Studio", "Cicada Studio"},
		{"  Cicada Studio ", "Cicada Studio"},
		{"", defaultTitle},
		{"   ", defaultTitle},
	} {
		if got := applicationName(tc.in); got != tc.want {
			t.Errorf("applicationName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	impl, _ := newPlatformApp(Options{Title: "Cicada Studio"})
	da := impl.(*darwinApp)
	if err := da.SetTitle("Cicada Studio - notes.cic"); err != nil {
		t.Fatal(err)
	}
	if da.appName != "Cicada Studio" {
		t.Errorf("appName = %q after SetTitle, want it unchanged: the menu is named for the app, not the document", da.appName)
	}
}

func TestDarwinProvidesApplicationMenu(t *testing.T) {
	submenu := &Menu{Items: []MenuItem{{Label: "Preferences"}}}
	for _, tc := range []struct {
		name  string
		items []MenuItem
		want  bool
	}{
		{"no items", nil, false},
		{"File first", []MenuItem{{Label: "File", Submenu: submenu}}, false},
		{"named for the app", []MenuItem{{Label: "Cicada Studio", Submenu: submenu}}, true},
		{"case does not matter", []MenuItem{{Label: "cicada STUDIO", Submenu: submenu}}, true},
		{"padding does not matter", []MenuItem{{Label: " Cicada Studio ", Submenu: submenu}}, true},
		{"mnemonic marker does not matter", []MenuItem{{Label: "&Cicada Studio", Submenu: submenu}}, true},
		{"an item that opens nothing is not a menu", []MenuItem{{Label: "Cicada Studio"}}, false},
		{"a separator is not a menu", []MenuItem{{Separator: true, Submenu: submenu}}, false},
		{"only the first counts", []MenuItem{{Label: "File", Submenu: submenu}, {Label: "Cicada Studio", Submenu: submenu}}, false},
	} {
		if got := providesApplicationMenu(tc.items, "Cicada Studio"); got != tc.want {
			t.Errorf("%s: providesApplicationMenu = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDarwinMenuRegistry(t *testing.T) {
	var r menuRegistry
	seen := map[int64]bool{}
	for i := 0; i < 100; i++ {
		tag := r.newTag()
		if tag < firstMenuTag {
			t.Fatalf("tag %d is below %d: 0 is what an item without a tag has", tag, firstMenuTag)
		}
		if seen[tag] {
			t.Fatalf("tag %d handed out twice", tag)
		}
		seen[tag] = true
	}

	first, second := r.newTag(), r.newTag()
	var calls []string
	r.replace(map[int64]func(){first: func() { calls = append(calls, "first") }, second: func() { calls = append(calls, "second") }})
	r.lookup(first)()
	r.lookup(second)()
	if !reflect.DeepEqual(calls, []string{"first", "second"}) {
		t.Errorf("handlers ran %v", calls)
	}
	if r.lookup(0) != nil || r.lookup(-1) != nil || r.lookup(second+1) != nil {
		t.Error("lookup found a handler for a tag nothing was registered under")
	}
	// Replacing the table drops the old handlers; the tags are not reused.
	third := r.newTag()
	r.replace(map[int64]func(){third: func() {}})
	if r.lookup(first) != nil || r.lookup(second) != nil {
		t.Error("handlers of the replaced menu bar are still registered")
	}
	if third <= second {
		t.Errorf("tag %d was handed out after %d: tags must not be reused", third, second)
	}
	if r.size() != 1 {
		t.Errorf("size = %d, want 1", r.size())
	}
	r.replace(nil)
	if r.size() != 0 || r.lookup(third) != nil {
		t.Error("replace(nil) left handlers behind")
	}
}

func TestDarwinMenuRegistryIsSafeForConcurrentUse(t *testing.T) {
	var r menuRegistry
	var wg sync.WaitGroup
	tags := make(chan int64, 8*50)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				tag := r.newTag()
				tags <- tag
				r.replace(map[int64]func(){tag: func() {}})
				r.lookup(tag)
			}
		}()
	}
	wg.Wait()
	close(tags)
	seen := map[int64]bool{}
	for tag := range tags {
		if seen[tag] {
			t.Fatalf("tag %d handed out twice", tag)
		}
		seen[tag] = true
	}
}

// The target class has the one method the stub implements, with the instance
// variable the recorder reads, and there is one instance for the process.
func TestDarwinMenuTargetClass(t *testing.T) {
	target, err := menuTarget()
	if err != nil {
		t.Fatalf("menuTarget: %v", err)
	}
	if target == 0 {
		t.Fatal("menuTarget returned nil")
	}
	again, err := menuTarget()
	if err != nil || again != target {
		t.Errorf("menuTarget = %#x, %v on the second call, want the same instance %#x", again, err, target)
	}
	cls := objcClass(menuTargetClassName)
	if cls == 0 {
		t.Fatalf("class %s is not registered", menuTargetClassName)
	}
	if off, err := ivarOffsetOf(cls); err != nil || off != delegateIvarOffset {
		t.Errorf("ctrl instance variable at %d, %v; the recorder reads offset %d", off, err, delegateIvarOffset)
	}
	if !sendBool1(cls, objcSel("instancesRespondToSelector:"), objcSel("menuAction:")) {
		t.Error("GoSXMenuTarget does not respond to menuAction:")
	}
	if sendBool1(cls, objcSel("instancesRespondToSelector:"), objcSel("windowShouldClose:")) {
		t.Error("GoSXMenuTarget claims windowShouldClose:, which belongs to the delegate")
	}
	if !sendBool1(target, objcSel("isKindOfClass:"), cls) {
		t.Error("the target is not an instance of GoSXMenuTarget")
	}
}

// menuAction: through the Objective-C runtime reaches the stub, which records the
// sender and retains it for Go.
func TestDarwinMenuActionRecordsItsSender(t *testing.T) {
	cls := objcClass(menuTargetClassName)
	if _, err := menuTarget(); err != nil || cls == 0 {
		t.Fatalf("menuTarget: %v", err)
	}
	r := newTestRing(t, 8)
	target, err := newRecorderObject(cls, r)
	if err != nil {
		t.Fatal(err)
	}
	defer objcRelease(target)
	sender := objcNew(objcClass("NSObject"))
	defer objcRelease(sender)

	send1(target, objcSel("menuAction:"), sender)
	if got := retainCount(sender); got != 2 {
		t.Errorf("sender retainCount = %d after the action, want 2 (ours and the record's)", got)
	}
	events := drainAll(r)
	if len(events) != 1 {
		t.Fatalf("drained %d records, want 1", len(events))
	}
	ev := events[0]
	if ev.kind != evMenuAction || ev.sel != objcSel("menuAction:") || ev.args[0] != sender || ev.mask != retainArg0 {
		t.Errorf("record = {kind %d sel %#x arg0 %#x mask %#x}, want a menuAction: record carrying the sender and retaining argument 0", ev.kind, ev.sel, ev.args[0], ev.mask)
	}
	if got := retainCount(sender); got != 1 {
		t.Errorf("sender retainCount = %d after the drain, want 1", got)
	}
}

// A click whose item the registry no longer knows, or that is not an item at
// all, must be ignored rather than crash the event pump.
func TestDarwinRunMenuActionToleratesUnknownSenders(t *testing.T) {
	impl, _ := newPlatformApp(Options{})
	da := impl.(*darwinApp)
	da.runMenuAction(0)
	withPool(func() {
		item := newMenuItem("x", 0, "")
		defer objcRelease(item)
		send1(item, objcSel("setTag:"), 987654)
		da.runMenuAction(item) // a tag nothing is registered under
		da.runMenuAction(nsString("not a menu item"))
	})

	var ran atomic.Int32
	tag := menuReg.newTag()
	menuReg.replace(map[int64]func(){tag: func() { ran.Add(1) }})
	defer menuReg.replace(nil)
	withPool(func() {
		item := newMenuItem("x", 0, "")
		defer objcRelease(item)
		send1(item, objcSel("setTag:"), uintptr(tag))
		da.runMenuAction(item)
	})
	if ran.Load() != 1 {
		t.Errorf("handler ran %d times, want 1", ran.Load())
	}
}

// SetMenuBar validates like the Windows backend: BuildMenuPlan's rules.
func TestDarwinSetMenuBarRejectsInvalidMenus(t *testing.T) {
	for _, tc := range []struct {
		name string
		menu Menu
	}{
		{"empty label", Menu{Items: []MenuItem{{Label: ""}}}},
		{"blank label", Menu{Items: []MenuItem{{Label: "   "}}}},
		{"NUL in a label", Menu{Items: []MenuItem{{Label: "Bad\x00"}}}},
		{"NUL in an id", Menu{Items: []MenuItem{{Label: "Bad", ID: "a\x00"}}}},
		{"tab in an accelerator", Menu{Items: []MenuItem{{Label: "Bad", Accelerator: "Cmd\tN"}}}},
		{"empty submenu", Menu{Items: []MenuItem{{Label: "File", Submenu: &Menu{}}}}},
		{"empty nested submenu", Menu{Items: []MenuItem{{Label: "File", Submenu: &Menu{Items: []MenuItem{{Label: "Recent", Submenu: &Menu{}}}}}}}},
	} {
		impl, _ := newPlatformApp(Options{})
		err := impl.SetMenuBar(tc.menu)
		if !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("%s: SetMenuBar = %v, want ErrInvalidOptions", tc.name, err)
		}
		if impl.(*darwinApp).menuBar != nil {
			t.Errorf("%s: the invalid menu was remembered", tc.name)
		}
	}

	// A menu nested past the limit BuildMenuPlan allows.
	deep := Menu{Items: []MenuItem{{Label: "leaf"}}}
	for i := 0; i < 10; i++ {
		deep = Menu{Items: []MenuItem{{Label: "level", Submenu: &deep}}}
	}
	impl, _ := newPlatformApp(Options{})
	if err := impl.SetMenuBar(deep); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("SetMenuBar(deeply nested) = %v, want ErrInvalidOptions", err)
	}
}

// Before Run there is no NSApplication: SetMenuBar remembers the latest menu for
// setup to install, and succeeds. Nothing may touch AppKit from a test goroutine.
func TestDarwinSetMenuBarBeforeRunIsRemembered(t *testing.T) {
	impl, _ := newPlatformApp(Options{Title: "Studio"})
	da := impl.(*darwinApp)
	first := Menu{Items: []MenuItem{{Label: "File", Submenu: &Menu{Items: []MenuItem{{Label: "New", OnClick: func() {}}}}}}}
	second := Menu{}
	for i, menu := range []Menu{first, second} {
		if err := impl.SetMenuBar(menu); err != nil {
			t.Fatalf("SetMenuBar #%d before Run = %v, want nil", i, err)
		}
	}
	if da.menuBar == nil || len(da.menuBar.Items) != 0 {
		t.Errorf("menuBar = %+v, want the latest (empty) menu", da.menuBar)
	}
	if da.menuInstalled {
		t.Error("a menu bar was installed without an application")
	}
	if menuReg.size() != 0 {
		t.Errorf("%d handlers registered before Run", menuReg.size())
	}
}

// ---- reading a live menu back --------------------------------------------

// menuReadout is one NSMenuItem as AppKit reports it.
type menuReadout struct {
	Title     string
	Key       string
	Mask      uintptr
	Enabled   bool
	State     uintptr
	Separator bool
	Action    uintptr
	Tag       uintptr
	Target    uintptr
	Items     []menuReadout // the submenu's items; nil without one
	Submenu   string        // the submenu's title; "" without one
	HasSub    bool
}

func readMenu(menu uintptr) []menuReadout {
	n := int(send0(menu, objcSel("numberOfItems")))
	out := make([]menuReadout, 0, n)
	for i := 0; i < n; i++ {
		item := send1(menu, objcSel("itemAtIndex:"), uintptr(i))
		r := menuReadout{
			Title:     goString(send0(item, objcSel("title"))),
			Key:       goString(send0(item, objcSel("keyEquivalent"))),
			Mask:      send0(item, objcSel("keyEquivalentModifierMask")),
			Enabled:   sendBool0(item, objcSel("isEnabled")),
			State:     send0(item, objcSel("state")),
			Separator: sendBool0(item, objcSel("isSeparatorItem")),
			Action:    send0(item, objcSel("action")),
			Tag:       send0(item, objcSel("tag")),
			Target:    send0(item, objcSel("target")),
		}
		if sub := send0(item, objcSel("submenu")); sub != 0 {
			r.HasSub = true
			r.Submenu = goString(send0(sub, objcSel("title")))
			r.Items = readMenu(sub)
		}
		out = append(out, r)
	}
	return out
}

// mainMenuOf reads [NSApp mainMenu] on the main thread.
func mainMenuOf(da *darwinApp) (items []menuReadout, ok bool) {
	da.onUIThread(func() error {
		withPool(func() {
			if menu := send0(da.nsapp, objcSel("mainMenu")); menu != 0 {
				items, ok = readMenu(menu), true
			}
		})
		return nil
	})
	return items, ok
}

// choose performs the menu item at path (indexes into the nested menus,
// starting at the main menu) as a click would: -[NSMenu
// performActionForItemAtIndex:] sends the item's action to its target.
func choose(da *darwinApp, path ...int) {
	da.onUIThread(func() error {
		withPool(func() {
			menu := send0(da.nsapp, objcSel("mainMenu"))
			for depth, index := range path {
				if depth == len(path)-1 {
					send1(menu, objcSel("performActionForItemAtIndex:"), uintptr(index))
					return
				}
				menu = send0(send1(menu, objcSel("itemAtIndex:"), uintptr(index)), objcSel("submenu"))
			}
		})
		return nil
	})
}

// titles of items, for readable failures.
func titlesOf(items []menuReadout) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
		if it.Separator {
			out[i] = "-"
		}
	}
	return out
}

// appMenuTitles is the standard application menu SetMenuBar puts in front of a
// menu that does not bring its own.
func appMenuTitles(name string) []string {
	return []string{"About " + name, "-", "Services", "-", "Hide " + name, "Hide Others", "Show All", "-", "Quit " + name}
}

func TestDarwinSetMenuBarBuildsTheNativeMenu(t *testing.T) {
	const name = "Menu Test"
	noop := func() {}
	recent := &Menu{Items: []MenuItem{{Label: "one.txt", OnClick: noop}, {Label: "two.txt", OnClick: noop}}}

	cases := []struct {
		name string
		menu Menu
		// titles of the main menu's items, then of each submenu's items
		bar  []string
		subs map[string][]string
		// check runs on the readout for details a title cannot show
		check func(t *testing.T, bar []menuReadout)
	}{
		{
			name: "an empty menu is the application menu alone",
			menu: Menu{},
			bar:  []string{name},
			subs: map[string][]string{name: appMenuTitles(name)},
		},
		{
			name: "a cross-platform menu gets the application menu in front",
			menu: Menu{Items: []MenuItem{
				{Label: "&File", Submenu: &Menu{Items: []MenuItem{
					{ID: "new", Label: "&New", Accelerator: "Cmd+N", OnClick: noop},
					{Separator: true},
					{ID: "recent", Label: "Open Recent", Submenu: recent},
					{ID: "exit", Label: "E&xit", Accelerator: "Ctrl+Q", OnClick: noop},
				}}},
				{Label: "View", Submenu: &Menu{Items: []MenuItem{{Label: "Zoom In", Accelerator: "Cmd++", OnClick: noop}}}},
				{Label: "Help", Submenu: &Menu{Items: []MenuItem{{Label: "Documentation", OnClick: noop}}}},
			}},
			bar: []string{name, "File", "View", "Help"},
			subs: map[string][]string{
				name:   appMenuTitles(name),
				"File": {"New", "-", "Open Recent", "Exit"},
				"View": {"Zoom In"},
				"Help": {"Documentation"},
			},
			check: func(t *testing.T, bar []menuReadout) {
				file := bar[1]
				if got := titlesOf(file.Items[2].Items); !reflect.DeepEqual(got, []string{"one.txt", "two.txt"}) {
					t.Errorf("Open Recent items = %v", got)
				}
				// AppKit gives an item that opens a submenu its own action
				// (submenuAction:); what matters is that it is not ours.
				if sub := file.Items[2]; sub.Submenu != "Open Recent" || sub.Action == objcSel("menuAction:") || sub.Tag != 0 {
					t.Errorf("Open Recent = %+v, want a submenu item that is not wired to the menu target", sub)
				}
				if n := file.Items[0]; n.Key != "n" || n.Mask != modCmd {
					t.Errorf("New key equivalent = %q mask %#x, want n with Command", n.Key, n.Mask)
				}
				if q := file.Items[3]; q.Key != "q" || q.Mask != modCtrl {
					t.Errorf("Exit key equivalent = %q mask %#x, want q with Control: Ctrl is Control", q.Key, q.Mask)
				}
				if z := bar[2].Items[0]; z.Key != "+" || z.Mask != modCmd {
					t.Errorf("Zoom In key equivalent = %q mask %#x, want + with Command", z.Key, z.Mask)
				}
				if d := bar[3].Items[0]; d.Key != "" {
					t.Errorf("Documentation key equivalent = %q, want none", d.Key)
				}
			},
		},
		{
			name: "a menu that opens with the application's name supplies its own application menu",
			menu: Menu{Items: []MenuItem{
				{Label: name, Submenu: &Menu{Items: []MenuItem{
					{Label: "Project Options", Accelerator: "Cmd+,", OnClick: noop},
					{Separator: true},
					{Label: "Quit Everything", Accelerator: "Cmd+Q", OnClick: noop},
				}}},
				{Label: "File", Submenu: &Menu{Items: []MenuItem{{Label: "Close", OnClick: noop}}}},
			}},
			bar: []string{name, "File"},
			subs: map[string][]string{
				name:   {"Project Options", "-", "Quit Everything"},
				"File": {"Close"},
			},
			check: func(t *testing.T, bar []menuReadout) {
				if p := bar[0].Items[0]; p.Key != "," || p.Mask != modCmd {
					t.Errorf("Project Options key equivalent = %q mask %#x, want , with Command", p.Key, p.Mask)
				}
			},
		},
		{
			name: "an item on the bar becomes a menu of one",
			menu: Menu{Items: []MenuItem{
				{Label: "Docs", OnClick: noop},
				{Separator: true}, // a bar has no separators
				{Label: "Help", Submenu: &Menu{Items: []MenuItem{{Label: "About", OnClick: noop}}}},
				{Label: "Off", Disabled: true, OnClick: noop},
				{Label: "Gone", Disabled: true, Submenu: &Menu{Items: []MenuItem{{Label: "Never", OnClick: noop}}}},
			}},
			bar:  []string{name, "Docs", "Help", "Off", "Gone"},
			subs: map[string][]string{name: appMenuTitles(name), "Docs": {"Docs"}, "Help": {"About"}, "Off": {"Off"}, "Gone": {"Never"}},
			check: func(t *testing.T, bar []menuReadout) {
				if d := bar[1].Items[0]; d.Action != objcSel("menuAction:") || d.Tag < firstMenuTag || d.Target == 0 {
					t.Errorf("Docs = %+v, want an item wired to the menu target", d)
				}
				if !bar[1].Enabled || !bar[2].Enabled {
					t.Errorf("Docs and Help are enabled on the bar: %v %v", bar[1].Enabled, bar[2].Enabled)
				}
				if bar[3].Enabled || bar[3].Items[0].Enabled {
					t.Errorf("Off = bar %v, item %v; want both disabled", bar[3].Enabled, bar[3].Items[0].Enabled)
				}
				if bar[4].Enabled {
					t.Error("Gone is enabled on the bar, want it disabled")
				}
			},
		},
		{
			name: "flags, and items that cannot be clicked",
			menu: Menu{Items: []MenuItem{{Label: "Tools", Submenu: &Menu{Items: []MenuItem{
				{Label: "Off", Disabled: true, OnClick: noop},
				{Label: "On", Checked: true, OnClick: noop},
				{Label: "Inert", Accelerator: "Cmd+J"},
				{Label: "Reload", Accelerator: "F5", OnClick: noop},
				{Label: "Bare key", Accelerator: "N", OnClick: noop},
				{Label: "Unknown key", Accelerator: "Cmd+Nonsense", OnClick: noop},
				{Label: "More", Disabled: true, Submenu: &Menu{Items: []MenuItem{{Label: "Deeper", OnClick: noop}}}},
			}}}}},
			bar: []string{name, "Tools"},
			subs: map[string][]string{
				name:    appMenuTitles(name),
				"Tools": {"Off", "On", "Inert", "Reload", "Bare key", "Unknown key", "More"},
			},
			check: func(t *testing.T, bar []menuReadout) {
				tools := bar[1].Items
				if off := tools[0]; off.Enabled || off.State != 0 {
					t.Errorf("Off = %+v, want disabled and unchecked", off)
				}
				if on := tools[1]; !on.Enabled || on.State != nsControlStateValueOn {
					t.Errorf("On = %+v, want enabled and checked", on)
				}
				if inert := tools[2]; inert.Action != 0 || inert.Target != 0 || inert.Key != "j" || inert.Mask != modCmd {
					t.Errorf("Inert = %+v, want no action but the shortcut shown", inert)
				}
				if reload := tools[3]; reload.Key != "\uf708" || reload.Mask != 0 || reload.Action != objcSel("menuAction:") {
					t.Errorf("Reload = {key %q mask %#x action %#x}, want F5 with no modifiers", reload.Key, reload.Mask, reload.Action)
				}
				if bare := tools[4]; bare.Key != "" {
					t.Errorf("Bare key = %q, want none: a plain letter would take keystrokes from the page", bare.Key)
				}
				if unknown := tools[5]; unknown.Key != "" || !unknown.Enabled || unknown.Action == 0 {
					t.Errorf("Unknown key = %+v, want a working item without a shortcut", unknown)
				}
				if more := tools[6]; more.Enabled || !more.HasSub || len(more.Items) != 1 {
					t.Errorf("More = %+v, want a disabled submenu item", more)
				}
				seen := map[uintptr]string{}
				for _, it := range tools {
					if it.Action == 0 {
						continue
					}
					if other, dup := seen[it.Tag]; dup {
						t.Errorf("%q and %q share tag %d", it.Title, other, it.Tag)
					}
					seen[it.Tag] = it.Title
				}
			},
		},
	}

	// One app, every menu bar in turn: each SetMenuBar replaces the whole bar.
	runTestApp(t, Options{Title: name}, func(app *App, da *darwinApp) {
		for _, tc := range cases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				if err := app.SetMenuBar(tc.menu); err != nil {
					t.Errorf("SetMenuBar: %v", err)
					return
				}
				bar, ok := mainMenuOf(da)
				if !ok {
					t.Error("the application has no main menu after SetMenuBar")
					return
				}
				if got := titlesOf(bar); !reflect.DeepEqual(got, tc.bar) {
					t.Errorf("top-level items = %q, want %q", got, tc.bar)
					return
				}
				for _, top := range bar {
					want, ok := tc.subs[top.Title]
					if !ok || want == nil {
						continue
					}
					if !top.HasSub || top.Submenu != top.Title {
						t.Errorf("%q: submenu title = %q (has submenu %v), want the item's own title", top.Title, top.Submenu, top.HasSub)
					}
					if got := titlesOf(top.Items); !reflect.DeepEqual(got, want) {
						t.Errorf("items of %q = %q, want %q", top.Title, got, want)
					}
				}
				if tc.check != nil {
					tc.check(t, bar)
				}
			})
		}
	})
}

// The synthesized application menu is the standard one: its actions travel the
// responder chain to NSApplication, with the keys macOS users expect.
func TestDarwinApplicationMenuHasTheStandardItems(t *testing.T) {
	const name = "Menu Test"
	runTestApp(t, Options{Title: name}, func(app *App, da *darwinApp) {
		if err := app.SetMenuBar(Menu{}); err != nil {
			t.Errorf("SetMenuBar: %v", err)
			return
		}
		bar, ok := mainMenuOf(da)
		if !ok || len(bar) != 1 {
			t.Errorf("main menu = %+v (%v), want just the application menu", titlesOf(bar), ok)
			return
		}
		items := bar[0].Items
		type want struct {
			title  string
			action string
			key    string
			mask   uintptr
		}
		for i, w := range []want{
			{"About " + name, "orderFrontStandardAboutPanel:", "", 0},
			{"-", "", "", 0},
			{"Services", "", "", 0},
			{"-", "", "", 0},
			{"Hide " + name, "hide:", "h", modCmd},
			{"Hide Others", "hideOtherApplications:", "h", modCmd | modOpt},
			{"Show All", "unhideAllApplications:", "", 0},
			{"-", "", "", 0},
			{"Quit " + name, "terminate:", "q", modCmd},
		} {
			if i >= len(items) {
				t.Errorf("the application menu has %d items, want %d", len(items), 9)
				return
			}
			got := items[i]
			title := got.Title
			if got.Separator {
				title = "-"
			}
			if title != w.title {
				t.Errorf("item %d title = %q, want %q", i, title, w.title)
				continue
			}
			if w.action != "" && got.Action != objcSel(w.action) {
				t.Errorf("%q action = %#x, want %s", w.title, got.Action, w.action)
			}
			if w.action != "" && got.Target != 0 {
				t.Errorf("%q has target %#x, want none (the responder chain reaches NSApplication)", w.title, got.Target)
			}
			if w.key != "" && (got.Key != w.key || got.Mask != w.mask) {
				t.Errorf("%q key = %q mask %#x, want %q mask %#x", w.title, got.Key, got.Mask, w.key, w.mask)
			}
		}
		if !items[2].HasSub {
			t.Error("Services has no submenu for AppKit to fill")
		}
		var servicesMenu uintptr
		da.onUIThread(func() error {
			withPool(func() { servicesMenu = send0(da.nsapp, objcSel("servicesMenu")) })
			return nil
		})
		if servicesMenu == 0 {
			t.Error("NSApp has no services menu: Services would stay empty")
		}
	})
}

// A click reaches OnClick on the main thread, through the recorder, the ring and
// the event pump; a disabled item, a stale one and one with no handler do not.
func TestDarwinMenuClicksReachOnClick(t *testing.T) {
	type click struct {
		name   string
		thread uint32
	}
	clicks := make(chan click, 16)
	on := func(name string) func() { return func() { clicks <- click{name, currentThreadID()} } }

	first := Menu{Items: []MenuItem{{Label: "File", Submenu: &Menu{Items: []MenuItem{
		{Label: "Open", Accelerator: "Cmd+O", OnClick: on("open")},
		{Separator: true},
		{Label: "Off", Disabled: true, OnClick: on("off")},
		{Label: "Inert"},
		{Label: "Recent", Submenu: &Menu{Items: []MenuItem{{Label: "one", OnClick: on("one")}}}},
	}}}, {Label: "Docs", OnClick: on("docs")}}}
	second := Menu{Items: []MenuItem{{Label: "File", Submenu: &Menu{Items: []MenuItem{{Label: "Open", OnClick: on("open2")}}}}}}

	next := func(t *testing.T, what string) (click, bool) {
		t.Helper()
		select {
		case c := <-clicks:
			return c, true
		case <-time.After(5 * time.Second):
			t.Errorf("no click arrived for %s", what)
			return click{}, false
		}
	}
	expectNone := func(t *testing.T, what string) {
		t.Helper()
		select {
		case c := <-clicks:
			t.Errorf("%s ran the handler %q, want none", what, c.name)
		case <-time.After(300 * time.Millisecond):
		}
	}

	// recorded counts the clicks the recorder saw, so that "nothing happened" can
	// be told from "the click never arrived".
	recorded := func() uint64 {
		ring, err := sharedRing()
		if err != nil {
			t.Fatal(err)
		}
		calls, _, _, _ := ring.stats()
		return calls
	}
	// ignored asserts that choosing the item got as far as the recorder (or, for
	// an item AppKit does not send, did not) and that no handler ran.
	ignored := func(t *testing.T, what string, reachesTheRecorder bool, choose func()) {
		t.Helper()
		before := recorded()
		choose()
		expectNone(t, what)
		if got := recorded() - before; reachesTheRecorder && got != 1 {
			t.Errorf("%s: the recorder saw %d clicks, want 1: the click should arrive and be ignored", what, got)
		} else if !reachesTheRecorder && got != 0 {
			t.Errorf("%s: the recorder saw %d clicks, want 0", what, got)
		}
	}

	runTestApp(t, Options{Title: "Click Test"}, func(app *App, da *darwinApp) {
		if err := app.SetMenuBar(first); err != nil {
			t.Errorf("SetMenuBar: %v", err)
			return
		}
		// Bar: 0 application, 1 File, 2 Docs.
		choose(da, 1, 0)
		if c, ok := next(t, "File > Open"); ok && (c.name != "open" || c.thread != dispatchMainThread) {
			t.Errorf("File > Open = %+v, want handler open on the main thread", c)
		}
		choose(da, 2, 0) // a bar item wrapped in a menu of one
		if c, ok := next(t, "Docs"); ok && c.name != "docs" {
			t.Errorf("Docs ran %q", c.name)
		}
		choose(da, 1, 4, 0) // File > Recent > one
		if c, ok := next(t, "File > Recent > one"); ok && c.name != "one" {
			t.Errorf("File > Recent > one ran %q", c.name)
		}
		ignored(t, "a disabled item", false, func() { choose(da, 1, 2) })         // File > Off
		ignored(t, "an item without OnClick", false, func() { choose(da, 1, 3) }) // File > Inert
		ignored(t, "a submenu item", false, func() { choose(da, 1, 4) })          // File > Recent

		// Replacing the menu bar replaces the handlers. Keep the old File menu
		// alive, so that a click recorded for it can still be made afterwards: its
		// tags must not find anything, nor the new menu's handler.
		var stale uintptr
		da.onUIThread(func() error {
			withPool(func() {
				bar := send0(da.nsapp, objcSel("mainMenu"))
				stale = objcRetain(send0(send1(bar, objcSel("itemAtIndex:"), 1), objcSel("submenu")))
			})
			return nil
		})
		defer da.onUIThread(func() error { objcRelease(stale); return nil })
		if err := app.SetMenuBar(second); err != nil {
			t.Errorf("SetMenuBar #2: %v", err)
			return
		}
		ignored(t, "an item of the replaced menu bar", true, func() {
			da.onUIThread(func() error {
				withPool(func() { send1(stale, objcSel("performActionForItemAtIndex:"), 0) })
				return nil
			})
		})
		choose(da, 1, 0)
		if c, ok := next(t, "the new File > Open"); ok && c.name != "open2" {
			t.Errorf("the new File > Open ran %q, want open2", c.name)
		}
		if n := menuReg.size(); n != 1 {
			t.Errorf("%d handlers registered, want only the one of the current menu bar", n)
		}
	})
}

// A menu bar given before Run is installed when the application starts, replaces
// the default menu, and works; the default comes back when Run ends so the next
// Run in the process does not inherit it.
func TestDarwinMenuBarBeforeRunAndRestoredAfter(t *testing.T) {
	const name = "Before Run"
	var clicked atomic.Int32
	var created []menuReadout
	var daRef atomic.Pointer[darwinApp]
	menu := Menu{Items: []MenuItem{{Label: "File", Submenu: &Menu{Items: []MenuItem{
		{Label: "Go", Accelerator: "Cmd+G", OnClick: func() { clicked.Add(1) }},
	}}}}}

	runTestAppBefore(t, Options{Title: name, OnWindowCreated: func(*Window) {
		// OnWindowCreated runs on the main thread inside setup, once the menu bar
		// given before Run is in place.
		created, _ = mainMenuOf(daRef.Load())
	}}, func(app *App, da *darwinApp) {
		daRef.Store(da)
		if err := app.SetMenuBar(menu); err != nil {
			t.Errorf("SetMenuBar before Run: %v", err)
		}
	}, func(app *App, da *darwinApp) {
		bar, ok := mainMenuOf(da)
		if !ok || !reflect.DeepEqual(titlesOf(bar), []string{name, "File"}) {
			t.Errorf("main menu while running = %q (%v), want the application menu and File", titlesOf(bar), ok)
		}
		choose(da, 1, 0)
		for deadline := time.Now().Add(5 * time.Second); clicked.Load() == 0 && time.Now().Before(deadline); {
			time.Sleep(5 * time.Millisecond)
		}
		if clicked.Load() != 1 {
			t.Errorf("File > Go ran %d times, want 1", clicked.Load())
		}
	})
	if got := titlesOf(created); !reflect.DeepEqual(got, []string{name, "File"}) {
		t.Errorf("main menu when the window was created = %q, want the application menu and File", got)
	}

	// After Run: the default menu bar is back, and the handlers are gone.
	var after []menuReadout
	onMain(func() {
		withPool(func() {
			app := send0(objcClass("NSApplication"), objcSel("sharedApplication"))
			if menu := send0(app, objcSel("mainMenu")); menu != 0 {
				after = readMenu(menu)
			}
		})
	})
	if len(after) != 1 || after[0].Title != name || !reflect.DeepEqual(titlesOf(after[0].Items), appMenuTitles(name)) {
		t.Errorf("main menu after Run = %q, want the default application menu alone", titlesOf(after))
	}
	if n := menuReg.size(); n != 0 {
		t.Errorf("%d menu handlers are still registered after Run", n)
	}
}

// One test goroutine, many menu bars: every SetMenuBar replaces the whole bar.
func TestDarwinSetMenuBarReplacesTheWholeBar(t *testing.T) {
	const name = "Replace Test"
	noop := func() {}
	bars := []Menu{
		{Items: []MenuItem{{Label: "A", Submenu: &Menu{Items: []MenuItem{{Label: "a1", OnClick: noop}}}}, {Label: "B", Submenu: &Menu{Items: []MenuItem{{Label: "b1", OnClick: noop}}}}}},
		{Items: []MenuItem{{Label: "C", Submenu: &Menu{Items: []MenuItem{{Label: "c1", OnClick: noop}}}}}},
		{},
		{Items: []MenuItem{{Label: "A", Submenu: &Menu{Items: []MenuItem{{Label: "a1", OnClick: noop}}}}}},
	}
	want := [][]string{{name, "A", "B"}, {name, "C"}, {name}, {name, "A"}}
	runTestApp(t, Options{Title: name}, func(app *App, da *darwinApp) {
		for i, menu := range bars {
			if err := app.SetMenuBar(menu); err != nil {
				t.Errorf("SetMenuBar #%d: %v", i, err)
				return
			}
			bar, _ := mainMenuOf(da)
			if got := titlesOf(bar); !reflect.DeepEqual(got, want[i]) {
				t.Errorf("bar #%d = %q, want %q", i, got, want[i])
			}
		}
		// From many goroutines at once, through the dispatcher.
		var wg sync.WaitGroup
		var failed atomic.Int32
		for g := 0; g < 6; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 10; i++ {
					if err := app.SetMenuBar(bars[i%len(bars)]); err != nil {
						failed.Add(1)
					}
				}
			}()
		}
		wg.Wait()
		if failed.Load() != 0 {
			t.Errorf("%d concurrent SetMenuBar calls failed", failed.Load())
		}
		if bar, ok := mainMenuOf(da); !ok || len(bar) < 1 || bar[0].Title != name {
			t.Errorf("main menu after the storm = %q", titlesOf(bar))
		}
	})
}

// A handler may replace the menu bar it was chosen from, the way a "Show Sidebar"
// toggle redraws its own check mark: the old menu goes away while its item is
// still being handled.
func TestDarwinMenuHandlerCanReplaceTheMenuBar(t *testing.T) {
	var toggles atomic.Int32
	var app *App
	var build func(checked bool) Menu
	build = func(checked bool) Menu {
		return Menu{Items: []MenuItem{{Label: "View", Submenu: &Menu{Items: []MenuItem{{
			Label: "Show Sidebar", Checked: checked, OnClick: func() {
				toggles.Add(1)
				if err := app.SetMenuBar(build(!checked)); err != nil {
					t.Errorf("SetMenuBar from a handler: %v", err)
				}
			},
		}}}}}}
	}
	runTestAppBefore(t, Options{Title: "Toggle Test"}, func(a *App, _ *darwinApp) {
		app = a
		if err := a.SetMenuBar(build(false)); err != nil {
			t.Errorf("SetMenuBar: %v", err)
		}
	}, func(a *App, da *darwinApp) {
		state := func() uintptr {
			bar, _ := mainMenuOf(da)
			if len(bar) < 2 || len(bar[1].Items) != 1 {
				t.Errorf("bar = %q", titlesOf(bar))
				return ^uintptr(0)
			}
			return bar[1].Items[0].State
		}
		wait := func(n int32) {
			for deadline := time.Now().Add(5 * time.Second); toggles.Load() < n && time.Now().Before(deadline); {
				time.Sleep(5 * time.Millisecond)
			}
		}
		want := uintptr(0)
		for i := int32(1); i <= 4; i++ {
			if got := state(); got != want {
				t.Errorf("before click %d: state = %d, want %d", i, got, want)
			}
			choose(da, 1, 0)
			wait(i)
			// The handler ran on the main thread and replaced the bar before the
			// next call here got its turn on it.
			want = 1 - want
		}
		if got := state(); got != want || toggles.Load() != 4 {
			t.Errorf("after four clicks: state = %d (want %d), handler ran %d times (want 4)", got, want, toggles.Load())
		}
	})
}

// A menu handler runs on the main thread inside the event pump; ShowMessage from
// there runs the alert's modal loop inside the pump's.
func TestDarwinShowMessageFromAMenuHandler(t *testing.T) {
	results := make(chan MessageResult, 1)
	alertHook = func(alert uintptr) func() { return pressAlertButton(t, alert, 0) }
	defer func() { alertHook = nil }()

	menu := Menu{Items: []MenuItem{{Label: "File", Submenu: &Menu{Items: []MenuItem{{Label: "Ask", OnClick: func() {
		got, err := ShowMessage(MessageOptions{Title: "From a handler", Text: "x", Buttons: MessageYesNo})
		if err != nil {
			t.Errorf("ShowMessage from a menu handler: %v", err)
		}
		results <- got
	}}}}}}}
	runTestApp(t, Options{Title: "Handler Alert"}, func(app *App, da *darwinApp) {
		if err := app.SetMenuBar(menu); err != nil {
			t.Errorf("SetMenuBar: %v", err)
			return
		}
		choose(da, 1, 0)
		select {
		case got := <-results:
			if got != MessageResultYes {
				t.Errorf("the handler's alert answered %q, want yes", got)
			}
		case <-time.After(10 * time.Second):
			t.Error("the alert in the menu handler never returned")
		}
	})
}
