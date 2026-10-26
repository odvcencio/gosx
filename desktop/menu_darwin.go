//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// The macOS menu bar: SetMenuBar turns a Menu into an NSMenu tree and makes it
// the application's main menu. DT1 slice of
// hypha://m31labs/gosx/initiatives/2026-10-17-darwin-desktop-backend.
//
// Clicks. A menu item reaches Go through the same path as a window delegate
// callback (ctrl_darwin.go): AppKit calls an Objective-C method as a C function
// on the main thread, and Go cannot be that function, so the method is an
// assembly stub that records the call in the native event ring and Go handles it
// after the event pump returns. The class is GoSXMenuTarget, registered once, with
// one method, menuAction:, and one instance (menuTarget) that is every
// OnClick item's target and lives for the process. The stub retains its
// argument, the sender NSMenuItem; the item's tag names the handler in menuReg,
// which is where the Go closures live (the Objective-C side holds integers only,
// so nothing the garbage collector owns is ever referenced from AppKit). The
// handler runs on the main thread, in the event pump (darwinApp.runMenuAction),
// like every other callback.
//
// Ownership. NSMenu and NSMenuItem are reference counted. Everything built here
// comes from alloc/init and is released as soon as its parent has taken it
// (addItem: and setSubmenu: retain), the root by setMainMenu:. The separators and
// strings come from class factories, autoreleased into the pool the install runs
// in.

// menuAccelerator is a parsed MenuItem.Accelerator: the key equivalent AppKit
// matches against the key's unmodified character, and the modifier mask.
type menuAccelerator struct {
	key  string
	mask uintptr
}

// menuModifiers names the modifier keys an accelerator may spell. Windows-style
// labels ("Ctrl+Shift+S") parse as written: ctrl is the Control key, not
// Command. cmdorctrl and mod are for labels written for every platform.
var menuModifiers = map[string]uintptr{
	"cmd":              nsEventModifierFlagCommand,
	"command":          nsEventModifierFlagCommand,
	"meta":             nsEventModifierFlagCommand,
	"super":            nsEventModifierFlagCommand,
	"cmdorctrl":        nsEventModifierFlagCommand,
	"commandorcontrol": nsEventModifierFlagCommand,
	"mod":              nsEventModifierFlagCommand,
	"ctrl":             nsEventModifierFlagControl,
	"control":          nsEventModifierFlagControl,
	"alt":              nsEventModifierFlagOption,
	"option":           nsEventModifierFlagOption,
	"opt":              nsEventModifierFlagOption,
	"shift":            nsEventModifierFlagShift,
}

// menuNamedKeys maps the names of non-printing keys to the character AppKit
// reports for them: ASCII control characters, and the private-use code points
// NSEvent.h defines for function keys (NSUpArrowFunctionKey and the rest).
var menuNamedKeys = map[string]string{
	"enter":     "\r",
	"return":    "\r",
	"tab":       "\t",
	"esc":       "\x1b",
	"escape":    "\x1b",
	"space":     " ",
	"backspace": "\x08",
	"delete":    "\uf728", // NSDeleteFunctionKey: forward delete
	"del":       "\uf728",
	"up":        "\uf700",
	"down":      "\uf701",
	"left":      "\uf702",
	"right":     "\uf703",
	"home":      "\uf729",
	"end":       "\uf72b",
	"pageup":    "\uf72c",
	"pagedown":  "\uf72d",
	"plus":      "+",
}

// functionKeyCharacter returns the character for "f1" to "f20" (NSF1FunctionKey
// is U+F704 and the rest follow it).
func functionKeyCharacter(name string) (string, bool) {
	if len(name) < 2 || name[0] != 'f' {
		return "", false
	}
	n := 0
	for _, c := range name[1:] {
		if c < '0' || c > '9' {
			return "", false
		}
		if n = n*10 + int(c-'0'); n > 20 {
			return "", false
		}
	}
	if n < 1 {
		return "", false
	}
	return string(rune(0xF704 + n - 1)), true
}

// parseMenuAccelerator reads a chord such as "Cmd+N", "Ctrl+Shift+Z", "Alt+F4",
// "Cmd+," or "Cmd++" (modifiers then one key, joined by "+", case-insensitive).
// It reports false for text it cannot use, and for a printable key with no
// Command, Control or Option modifier: AppKit would offer every plain keystroke
// to the menu bar before the web view sees it, and the accelerator was only ever
// a label on the platforms where the page handles its own keys. Function keys
// may stand alone.
func parseMenuAccelerator(text string) (menuAccelerator, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return menuAccelerator{}, false
	}
	parts := strings.Split(text, "+")
	last := len(parts) - 1
	keyText, modifiers := strings.TrimSpace(parts[last]), parts[:last]
	if keyText == "" && last >= 1 && strings.TrimSpace(parts[last-1]) == "" {
		keyText, modifiers = "+", parts[:last-1] // "Cmd++": the key is the plus sign
	}
	var mask uintptr
	for _, name := range modifiers {
		flag, ok := menuModifiers[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return menuAccelerator{}, false
		}
		mask |= flag
	}

	lower := strings.ToLower(keyText)
	var key string
	standalone := false
	if r, size := utf8.DecodeRuneInString(keyText); size == len(keyText) && size > 0 && r != utf8.RuneError {
		if r < ' ' || r == 0x7f {
			return menuAccelerator{}, false
		}
		key = string(unicode.ToLower(r))
	} else if named, ok := menuNamedKeys[lower]; ok {
		key = named
	} else if named, ok := functionKeyCharacter(lower); ok {
		key, standalone = named, true
	} else {
		return menuAccelerator{}, false
	}
	if !standalone && mask&(nsEventModifierFlagCommand|nsEventModifierFlagControl|nsEventModifierFlagOption) == 0 {
		return menuAccelerator{}, false
	}
	return menuAccelerator{key: key, mask: mask}, true
}

// menuTitle turns a Menu label into the title AppKit shows. Labels follow the
// Win32 convention the Windows backend gives them: "&File" marks F as a
// mnemonic and "&&" is a literal ampersand. macOS menus have no mnemonics, so
// the marker is dropped.
func menuTitle(label string) string {
	label = strings.TrimSpace(label)
	if !strings.Contains(label, "&") {
		return label
	}
	var b strings.Builder
	for i := 0; i < len(label); i++ {
		if label[i] != '&' {
			b.WriteByte(label[i])
			continue
		}
		if i+1 < len(label) && label[i+1] == '&' {
			b.WriteByte('&')
			i++
		}
	}
	return b.String()
}

// applicationName is the name the application menu carries: the title the app
// was created with, or the package default.
func applicationName(title string) string {
	if title = strings.TrimSpace(title); title != "" {
		return title
	}
	return defaultTitle
}

// providesApplicationMenu reports whether the caller wrote the application menu
// themselves: the first menu of a macOS menu bar is the application's, titled
// with its name. A cross-platform Menu does not carry that convention (its first
// menu is File), so unless it opens with a submenu named for the app, SetMenuBar
// puts a standard application menu in front, which is where Quit, Hide and About
// live.
func providesApplicationMenu(items []MenuItem, appName string) bool {
	if len(items) == 0 || items[0].Separator || items[0].Submenu == nil {
		return false
	}
	return strings.EqualFold(menuTitle(items[0].Label), strings.TrimSpace(appName))
}

// menuRegistry is the table of OnClick handlers for the installed menu bar,
// keyed by the tag of the NSMenuItem that triggers each. Tags are never reused,
// so a click recorded for an item of a menu bar that has since been replaced
// finds no handler instead of the wrong one.
type menuRegistry struct {
	mu      sync.Mutex
	next    int64
	actions map[int64]func()
}

// menuReg is the process-wide registry: NSApplication has one main menu per
// process, and the one GoSXMenuTarget instance serves it.
var menuReg menuRegistry

// firstMenuTag is the first tag handed out. Tag 0 is what an item has when
// nothing set one.
const firstMenuTag = 1000

func (r *menuRegistry) newTag() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.next < firstMenuTag {
		r.next = firstMenuTag
	}
	tag := r.next
	r.next++
	return tag
}

// replace installs the handlers of a new menu bar and drops the old ones.
func (r *menuRegistry) replace(actions map[int64]func()) {
	r.mu.Lock()
	r.actions = actions
	r.mu.Unlock()
}

func (r *menuRegistry) lookup(tag int64) func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.actions[tag]
}

func (r *menuRegistry) size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.actions)
}

const (
	menuTargetClassName = "GoSXMenuTarget"
	menuActionSelector  = "menuAction:"
)

var (
	menuTargetOnce sync.Once
	menuTargetObj  uintptr
	menuTargetErr  error
)

// menuTarget returns the one GoSXMenuTarget instance, registering the class on
// first use. The instance's ctrl variable points at the process-wide event ring,
// and it is never released: menu items refer to their target without retaining
// it, for as long as the menu bar lives.
func menuTarget() (uintptr, error) {
	menuTargetOnce.Do(func() {
		ring, err := sharedRing()
		if err != nil {
			menuTargetErr = err
			return
		}
		cls, err := registerRecorderClass(menuTargetClassName, []recorderMethod{
			{menuActionSelector, impStubs[impStubMenuAction], "v@:@"},
		})
		if err != nil {
			menuTargetErr = err
			return
		}
		menuTargetObj, menuTargetErr = newRecorderObject(cls, ring)
	})
	return menuTargetObj, menuTargetErr
}

// runMenuAction handles an evMenuAction record: sender is the NSMenuItem that
// was chosen, which the recorder retained for this call. It runs on the main
// thread, in the event pump.
func (a *darwinApp) runMenuAction(sender uintptr) {
	if sender == 0 || !respondsTo(sender, "tag") {
		return
	}
	if handler := menuReg.lookup(int64(send0(sender, objcSel("tag")))); handler != nil {
		handler()
	}
}

// newNSMenu returns an owned NSMenu with the given title and no items, or 0.
func newNSMenu(title string) uintptr {
	return send1(send0(objcClass("NSMenu"), objcSel("alloc")), objcSel("initWithTitle:"), nsString(title))
}

// newMenuItem returns an owned NSMenuItem, or 0. action is a selector (0 for
// none).
func newMenuItem(title string, action uintptr, key string) uintptr {
	return send3(send0(objcClass("NSMenuItem"), objcSel("alloc")), objcSel("initWithTitle:action:keyEquivalent:"),
		nsString(title), action, nsString(key))
}

// addMenuItem appends item to menu, which retains it, and drops the caller's
// reference.
func addMenuItem(menu, item uintptr) {
	send1(menu, objcSel("addItem:"), item)
	objcRelease(item)
}

func addSeparator(menu uintptr) {
	send1(menu, objcSel("addItem:"), send0(objcClass("NSMenuItem"), objcSel("separatorItem")))
}

// newApplicationMenuItem returns an owned top-level item holding the standard
// application menu:
//
//	About <name>
//	-
//	Services        (AppKit fills it in)
//	-
//	Hide <name>     Command-H
//	Hide Others     Option-Command-H
//	Show All
//	-
//	Quit <name>     Command-Q
//
// The actions have no target, so they travel the responder chain to
// NSApplication; Quit reaches the delegate's applicationShouldTerminate:, which
// makes it the same close path as the window's red button.
func newApplicationMenuItem(app uintptr, name string) uintptr {
	menu := newNSMenu(name)
	if menu == 0 {
		return 0
	}
	defer objcRelease(menu)
	standard := func(title, action, key string, mask uintptr) {
		item := newMenuItem(title, objcSel(action), key)
		if item == 0 {
			return
		}
		if key != "" {
			send1(item, objcSel("setKeyEquivalentModifierMask:"), mask)
		}
		addMenuItem(menu, item)
	}

	standard("About "+name, "orderFrontStandardAboutPanel:", "", 0)
	addSeparator(menu)
	if services, servicesMenu := newMenuItem("Services", 0, ""), newNSMenu("Services"); services != 0 && servicesMenu != 0 {
		send1(services, objcSel("setSubmenu:"), servicesMenu)
		addMenuItem(menu, services)
		if app != 0 {
			send1(app, objcSel("setServicesMenu:"), servicesMenu)
		}
		objcRelease(servicesMenu)
	} else {
		objcRelease(services)
		objcRelease(servicesMenu)
	}
	addSeparator(menu)
	standard("Hide "+name, "hide:", "h", nsEventModifierFlagCommand)
	standard("Hide Others", "hideOtherApplications:", "h", nsEventModifierFlagCommand|nsEventModifierFlagOption)
	standard("Show All", "unhideAllApplications:", "", 0)
	addSeparator(menu)
	standard("Quit "+name, "terminate:", "q", nsEventModifierFlagCommand)

	item := newMenuItem(name, 0, "")
	if item == 0 {
		return 0
	}
	send1(item, objcSel("setSubmenu:"), menu)
	return item
}

// installApplicationMenu gives the process the menu bar it has until SetMenuBar
// replaces it: the standard application menu alone. AppKit does not synthesize
// one for a process without a bundle, and without it Command-Q and Command-H
// reach nothing. It runs on the main thread, inside an autorelease pool.
func installApplicationMenu(app uintptr, name string) {
	if objcClass("NSMenu") == 0 || objcClass("NSMenuItem") == 0 {
		return
	}
	root := newNSMenu("")
	if root == 0 {
		return
	}
	defer objcRelease(root) // setMainMenu: retains it
	if item := newApplicationMenuItem(app, name); item != 0 {
		addMenuItem(root, item)
	}
	send1(app, objcSel("setMainMenu:"), root)
}

// menuBuilder turns a Menu into NSMenu objects. It collects the OnClick handlers
// of the items it builds in actions; they replace the registry's only once the
// whole menu bar has been built and installed.
type menuBuilder struct {
	app     uintptr
	target  uintptr
	appName string
	actions map[int64]func()
}

func errMenuObject(what string) error {
	return fmt.Errorf("desktop: could not create the native menu %s", what)
}

// build returns the owned main menu for menu: the application menu, unless menu
// supplies its own, then the caller's menus in order.
func (b *menuBuilder) build(menu Menu) (root uintptr, err error) {
	if objcClass("NSMenu") == 0 || objcClass("NSMenuItem") == 0 {
		return 0, fmt.Errorf("%w: AppKit menus are unavailable", ErrUnsupported)
	}
	root = newNSMenu("")
	if root == 0 {
		return 0, errMenuObject("bar")
	}
	defer func() {
		if err != nil {
			objcRelease(root)
			root = 0
		}
	}()
	// A disabled menu on the bar (MenuItem.Disabled) stays disabled.
	send1(root, objcSel("setAutoenablesItems:"), 0)
	if !providesApplicationMenu(menu.Items, b.appName) {
		item := newApplicationMenuItem(b.app, b.appName)
		if item == 0 {
			return root, errMenuObject("application menu")
		}
		addMenuItem(root, item)
	}
	for _, entry := range menu.Items {
		if entry.Separator {
			continue // a menu bar has no separators
		}
		item, err := b.topLevelItem(entry)
		if err != nil {
			return root, err
		}
		addMenuItem(root, item)
	}
	return root, nil
}

// topLevelItem builds one entry of the menu bar. The bar can only hold menus,
// so a plain item at the top level (which the Windows bar allows) becomes a menu
// of the same name holding that one item.
func (b *menuBuilder) topLevelItem(entry MenuItem) (uintptr, error) {
	if entry.Submenu != nil {
		return b.submenuItem(entry)
	}
	title := menuTitle(entry.Label)
	menu := newNSMenu(title)
	if menu == 0 {
		return 0, errMenuObject("menu " + title)
	}
	defer objcRelease(menu)
	send1(menu, objcSel("setAutoenablesItems:"), 0)
	leaf, err := b.leafItem(entry)
	if err != nil {
		return 0, err
	}
	addMenuItem(menu, leaf)
	holder := newMenuItem(title, 0, "")
	if holder == 0 {
		return 0, errMenuObject("item " + title)
	}
	send1(holder, objcSel("setSubmenu:"), menu)
	if entry.Disabled {
		send1(holder, objcSel("setEnabled:"), 0)
	}
	return holder, nil
}

// submenuItem builds an item that opens the entry's submenu.
func (b *menuBuilder) submenuItem(entry MenuItem) (uintptr, error) {
	title := menuTitle(entry.Label)
	menu := newNSMenu(title)
	if menu == 0 {
		return 0, errMenuObject("menu " + title)
	}
	defer objcRelease(menu)
	// The items' enabled state is the caller's (MenuItem.Disabled), not what
	// AppKit works out from the responder chain.
	send1(menu, objcSel("setAutoenablesItems:"), 0)
	for _, child := range entry.Submenu.Items {
		var item uintptr
		var err error
		switch {
		case child.Separator:
			addSeparator(menu)
			continue
		case child.Submenu != nil:
			item, err = b.submenuItem(child)
		default:
			item, err = b.leafItem(child)
		}
		if err != nil {
			return 0, err
		}
		addMenuItem(menu, item)
	}
	item := newMenuItem(title, 0, "")
	if item == 0 {
		return 0, errMenuObject("item " + title)
	}
	send1(item, objcSel("setSubmenu:"), menu)
	if entry.Disabled {
		send1(item, objcSel("setEnabled:"), 0)
	}
	return item, nil
}

// leafItem builds a plain item. With an OnClick, its action is the target's
// menuAction: and its tag names the handler. Without one it has no action, so
// its key equivalent (if it has one) is shown but not claimed from the page.
func (b *menuBuilder) leafItem(entry MenuItem) (uintptr, error) {
	title := menuTitle(entry.Label)
	action := uintptr(0)
	if entry.Role != "" {
		action = objcSel(string(entry.Role) + ":")
	} else if entry.OnClick != nil {
		action = objcSel(menuActionSelector)
	}
	item := newMenuItem(title, action, "")
	if item == 0 {
		return 0, errMenuObject("item " + title)
	}
	if entry.Role == "" && entry.OnClick != nil {
		tag := menuReg.newTag()
		b.actions[tag] = entry.OnClick
		send1(item, objcSel("setTag:"), uintptr(tag))
		send1(item, objcSel("setTarget:"), b.target)
	}
	if accelerator, ok := parseMenuAccelerator(entry.Accelerator); ok {
		send1(item, objcSel("setKeyEquivalent:"), nsString(accelerator.key))
		send1(item, objcSel("setKeyEquivalentModifierMask:"), accelerator.mask)
	}
	if entry.Checked {
		send1(item, objcSel("setState:"), nsControlStateValueOn)
	}
	if entry.Disabled {
		send1(item, objcSel("setEnabled:"), 0)
	}
	return item, nil
}

// SetMenuBar installs menu as the application's menu bar. Before Run it is
// remembered and installed when the application starts; after Run, it replaces
// the menu bar at once, on the main thread. The replacement is whole: the
// handlers of the previous menu bar stop firing. See providesApplicationMenu for
// the application menu, menuTitle for labels and parseMenuAccelerator for
// Accelerator.
func (a *darwinApp) SetMenuBar(menu Menu) error {
	if _, err := BuildMenuPlan(menu); err != nil {
		return err
	}
	a.mu.Lock()
	a.menuBar = &menu
	a.mu.Unlock()
	return a.onUIThread(a.installMenuBarOnUI)
}

// installMenuBarOnUI installs the latest menu SetMenuBar was given, if the
// application is running and the caller is the main thread. Otherwise there is
// nothing to do yet: setup installs a.menuBar once the dispatcher is running, so
// a call that arrives while setup is still under way is not lost.
func (a *darwinApp) installMenuBarOnUI() error {
	a.mu.Lock()
	app, menu := a.nsapp, a.menuBar
	a.mu.Unlock()
	if app == 0 || menu == nil || pthreadMainNP() == 0 {
		return nil
	}
	var err error
	withPool(func() { err = a.installMenuBar(app, *menu) })
	return err
}

// installMenuBar builds menu and makes it the main menu of app, on the main
// thread, inside an autorelease pool.
func (a *darwinApp) installMenuBar(app uintptr, menu Menu) error {
	target, err := menuTarget()
	if err != nil {
		return err
	}
	b := &menuBuilder{app: app, target: target, appName: a.appName, actions: map[int64]func(){}}
	root, err := b.build(menu)
	if err != nil {
		return err
	}
	defer objcRelease(root) // setMainMenu: retains it
	send1(app, objcSel("setMainMenu:"), root)
	menuReg.replace(b.actions)
	a.menuInstalled = true
	return nil
}

// restoreApplicationMenu puts the default menu bar back after a Run that
// installed its own. NSApplication outlives the Run, and its menu must not keep
// handlers of an application that is gone (or be inherited by the next one).
func (a *darwinApp) restoreApplicationMenu(app uintptr) {
	if !a.menuInstalled {
		return
	}
	a.menuInstalled = false
	menuReg.replace(nil)
	installApplicationMenu(app, a.appName)
}
