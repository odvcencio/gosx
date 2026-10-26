//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDarwinFileDialogExtensions(t *testing.T) {
	for _, tc := range []struct {
		name, pattern, ext string
		want               []string
		other, invalid     bool
	}{
		{"empty", "", "", nil, true, false},
		{"default", "", ".cicada", []string{"cicada"}, true, false},
		{"score", "*.cicada", "cicada", []string{"cicada"}, false, false},
		{"multiple", "*.wav;*.aiff;*.wav", "", []string{"wav", "aiff"}, false, false},
		{"all files", "*.*", "", nil, true, false},
		{"glob", "song?.cicada", "", nil, false, true},
		{"path", "../score", "", nil, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var filters []FileFilter
			if tc.pattern != "" {
				filters = []FileFilter{{Pattern: tc.pattern}}
			}
			got, other, err := fileDialogExtensions(filters, tc.ext)
			if tc.invalid {
				if !errors.Is(err, ErrInvalidOptions) {
					t.Fatalf("invalid filter: %v", err)
				}
			} else if err != nil || !reflect.DeepEqual(got, tc.want) || other != tc.other {
				t.Fatalf("extensions=%v allowOther=%v err=%v, want %v/%v", got, other, err, tc.want, tc.other)
			}
		})
	}
}

func TestDarwinFilePanelsConfigureAndCancel(t *testing.T) {
	requireGUI(t)
	onMain(func() {
		withPool(func() {
			send0(objcClass("NSApplication"), objcSel("sharedApplication"))
			impl, _ := newPlatformApp(Options{})
			a := impl.(*darwinApp)
			old := runFilePanel
			defer func() { runFilePanel = old }()
			calls := 0
			runFilePanel = func(panel uintptr) uintptr {
				calls++
				if got, _ := cfStringToString(send0(panel, objcSel("title"))); got != "Choose score" {
					t.Errorf("panel title = %q", got)
				}
				if sendBool0(panel, objcSel("allowsOtherFileTypes")) {
					t.Error("filtered score panel allows other types")
				}
				if got := send0(send0(panel, objcSel("allowedFileTypes")), objcSel("count")); got != 1 {
					t.Errorf("allowed extension count = %d", got)
				}
				if calls == 1 && (!sendBool0(panel, objcSel("allowsMultipleSelection")) || sendBool0(panel, objcSel("canChooseDirectories"))) {
					t.Error("open panel selection flags were not applied")
				}
				return 0 // Cancel; never show a modal dialog.
			}
			filters := []FileFilter{{Name: "Cicada score", Pattern: "*.cicada"}}
			paths, err := a.OpenFileDialog(OpenFileOptions{Title: "Choose score", Filters: filters, AllowMultiple: true})
			if err != nil || len(paths) != 0 {
				t.Errorf("cancelled open = %v, %v", paths, err)
			}
			path, err := a.SaveFileDialog(SaveFileOptions{Title: "Choose score", Filters: filters, InitialFilename: "new.cicada"})
			if err != nil || path != "" || calls != 2 {
				t.Errorf("cancelled save = %q, %v (calls=%d)", path, err, calls)
			}
		})
	})
}

func TestDarwinFinderDocumentsReachCallback(t *testing.T) {
	onMain(func() {
		withPool(func() {
			ring := newTestRing(t, 8)
			cls, err := delegateClass()
			if err != nil {
				t.Error(err)
				return
			}
			delegate, err := newDelegate(cls, ring)
			if err != nil {
				t.Error(err)
				return
			}
			defer objcRelease(delegate)
			var received [][]string
			a := &darwinApp{ring: ring, options: Options{OnOpenFiles: func(paths []string) { received = append(received, paths) }}}
			files := nsArray([]uintptr{nsString("/tmp/a song.cicada"), nsString("relative.cicada")})
			send2(delegate, objcSel("application:openFiles:"), 0, files)
			a.drainNativeEvents()
			urls := nsArray([]uintptr{nsURL("https://example.com/remote.cicada"), nsURL("file:///tmp/another%20song.cicada")})
			send2(delegate, objcSel("application:openURLs:"), 0, urls)
			a.drainNativeEvents()
			want := [][]string{{"/tmp/a song.cicada"}, {"/tmp/another song.cicada"}}
			if !reflect.DeepEqual(received, want) {
				t.Errorf("Finder delivered %v, want %v", received, want)
			}
			a.closing = true
			send2(delegate, objcSel("application:openFiles:"), 0, files)
			a.drainNativeEvents()
			if len(received) != 2 {
				t.Error("documents delivered after close began")
			}
			_, _, retained, released := ring.stats()
			if retained != released {
				t.Errorf("document arrays leaked: retained=%d released=%d", retained, released)
			}
		})
	})
}

func TestDarwinEditRoleUsesResponderChain(t *testing.T) {
	onMain(func() {
		withPool(func() {
			b := &menuBuilder{actions: map[int64]func(){}}
			item, err := b.leafItem(MenuItem{Label: "Copy", Role: MenuRoleCopy, Accelerator: "Cmd+C", OnClick: func() { t.Error("role used fallback") }})
			if err != nil {
				t.Error(err)
				return
			}
			defer objcRelease(item)
			if send0(item, objcSel("action")) != objcSel("copy:") || send0(item, objcSel("target")) != 0 || len(b.actions) != 0 {
				t.Error("Copy is not routed to the native responder chain")
			}
			if got, _ := cfStringToString(send0(item, objcSel("keyEquivalent"))); got != "c" {
				t.Errorf("Copy key equivalent = %q", got)
			}
		})
	})
}

func TestDarwinFinderLaunchServicesDelivery(t *testing.T) {
	requireGUI(t)
	var bundle string
	onMain(func() {
		withPool(func() {
			main := send0(objcClass("NSBundle"), objcSel("mainBundle"))
			bundle, _ = cfStringToString(send0(main, objcSel("bundlePath")))
		})
	})
	if !strings.HasSuffix(bundle, ".app") {
		t.Skip("requires a test .app launched through LaunchServices")
	}
	file := filepath.Join(t.TempDir(), "Finder song.cicada")
	if err := os.WriteFile(file, []byte("// disposable document\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatal(err)
	}
	opened := make(chan []string, 1)
	runTestApp(t, Options{OnOpenFiles: func(paths []string) { opened <- paths }}, func(_ *App, _ *darwinApp) {
		if output, err := exec.Command("/usr/bin/open", "-a", bundle, file).CombinedOutput(); err != nil {
			t.Errorf("LaunchServices open: %v: %s", err, output)
			return
		}
		select {
		case paths := <-opened:
			if !reflect.DeepEqual(paths, []string{canonical}) {
				t.Errorf("Finder opened %v, want %q", paths, file)
			}
		case <-time.After(5 * time.Second):
			t.Error("Finder did not deliver the document to the running app")
		}
	})
}
