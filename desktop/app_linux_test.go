//go:build linux && (amd64 || arm64)

package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/ebitengine/purego"
	"testing"
	"time"
)

// Opt in only on a Linux GUI test machine with WebKitGTK 4.1 and a display.
func TestLinuxWebKitSmoke(t *testing.T) {
	if os.Getenv("GOSX_LINUX_GUI_TEST") != "1" {
		t.Skip("set GOSX_LINUX_GUI_TEST=1 with WebKitGTK and a display")
	}
	messages := make(chan string, 2)
	navigated := make(chan NavigationCompleted, 2)
	closed := make(chan struct{}, 1)
	activated := make(chan struct{}, 1)
	profile := filepath.Join(t.TempDir(), "profile")
	app, err := New(Options{Title: "GoSX Linux smoke", Width: 720, Height: 480, UserDataDir: profile, HTML: `<!doctype html><title>GoSX Linux smoke</title><h1>GoSX Linux desktop</h1><script>window.chrome.webview.postMessage("ready")</script>`, OnWebMessage: func(m string) { messages <- m }, OnNavigationCompleted: func(n NavigationCompleted) { navigated <- n }, OnClose: func() { closed <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	fileMenu := &Menu{Items: []MenuItem{{Label: "&Test", OnClick: func() { activated <- struct{}{} }}}}
	if err := app.SetMenuBar(Menu{Items: []MenuItem{{Label: "&File", Submenu: fileMenu}}}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- app.Run() }()
	defer app.Close()
	select {
	case msg := <-messages:
		if msg != "ready" {
			t.Fatalf("message %q", msg)
		}
	case err := <-done:
		t.Fatalf("window exited before message: %v", err)
	case <-time.After(25 * time.Second):
		t.Fatal("timed out waiting for WebKit message")
	}
	select {
	case n := <-navigated:
		if !n.Success {
			t.Fatalf("navigation: %+v", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("missing navigation callback")
	}
	if _, err := os.Stat(filepath.Join(profile, "data")); err != nil {
		t.Fatalf("isolated profile: %v", err)
	}
	lib, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW)
	if err != nil {
		t.Fatal(err)
	}
	var children func(uintptr) uintptr
	var submenu func(uintptr) uintptr
	var activate func(uintptr)
	purego.RegisterLibFunc(&children, lib, "gtk_container_get_children")
	purego.RegisterLibFunc(&submenu, lib, "gtk_menu_item_get_submenu")
	purego.RegisterLibFunc(&activate, lib, "gtk_menu_item_activate")
	type gList struct{ data, next, prev uintptr }
	native := app.impl.(*linuxApp)
	if err := native.onUI(func(g *gtkAPI, w, v uintptr) error {
		top := children(native.menuBar)
		if top == 0 {
			return fmt.Errorf("menu bar has no children")
		}
		sub := submenu((*gList)(unsafe.Pointer(top)).data)
		items := children(sub)
		if items == 0 {
			return fmt.Errorf("menu has no items")
		}
		activate((*gList)(unsafe.Pointer(items)).data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-activated:
	case <-time.After(5 * time.Second):
		t.Fatal("native menu activation failed")
	}
	if err := app.ExecuteScript(`window.chrome.webview.postMessage("script")`); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-messages:
		if msg != "script" {
			t.Fatalf("script response %q", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("missing script message")
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("window did not close")
	}
	select {
	case <-closed:
	default:
		t.Fatal("missing close callback")
	}
}
