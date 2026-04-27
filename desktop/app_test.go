package desktop

import (
	"errors"
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/odvcencio/gosx/desktop/bridge"
)

func TestNormalizeOptionsDefaults(t *testing.T) {
	options, err := normalizeOptions(Options{})
	if err != nil {
		t.Fatalf("normalize options: %v", err)
	}
	if options.Title != defaultTitle {
		t.Fatalf("default title = %q, want %q", options.Title, defaultTitle)
	}
	if options.Width != defaultWidth {
		t.Fatalf("default width = %d, want %d", options.Width, defaultWidth)
	}
	if options.Height != defaultHeight {
		t.Fatalf("default height = %d, want %d", options.Height, defaultHeight)
	}
	if options.URL != defaultURL {
		t.Fatalf("default url = %q, want %q", options.URL, defaultURL)
	}
	if options.AppID != "gosx.gosx" {
		t.Fatalf("default app id = %q, want gosx.gosx", options.AppID)
	}
}

func TestNormalizeOptionsRejectsNUL(t *testing.T) {
	_, err := normalizeOptions(Options{Title: "bad\x00title"})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("error = %v, want ErrInvalidOptions", err)
	}
}

func TestNormalizeOptionsRejectsURLAndHTML(t *testing.T) {
	_, err := normalizeOptions(Options{URL: "https://example.test", HTML: "<h1>ok</h1>"})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("error = %v, want ErrInvalidOptions", err)
	}
}

func TestNormalizeOptionsRejectsBadAppID(t *testing.T) {
	_, err := normalizeOptions(Options{AppID: `bad\id`})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("error = %v, want ErrInvalidOptions", err)
	}
}

func TestNormalizeWindowOptions(t *testing.T) {
	base, err := normalizeOptions(Options{Title: "Base", Width: 900, Height: 700})
	if err != nil {
		t.Fatalf("normalize base: %v", err)
	}
	got, err := normalizeWindowOptions(base, WindowOptions{})
	if err != nil {
		t.Fatalf("normalize window: %v", err)
	}
	if got.Title != "Base" || got.Width != 900 || got.Height != 700 || got.URL != defaultURL {
		t.Fatalf("window options = %+v", got)
	}
}

func TestDevToolsEnabled(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want bool
	}{
		{name: "off", opts: Options{}, want: false},
		{name: "debug", opts: Options{Debug: true}, want: true},
		{name: "devtools", opts: Options{DevTools: true}, want: true},
		{name: "both", opts: Options{Debug: true, DevTools: true}, want: true},
	}
	for _, tc := range cases {
		if got := devToolsEnabled(tc.opts); got != tc.want {
			t.Fatalf("%s: devToolsEnabled = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNativeBridgeMethodsDispatchToApp(t *testing.T) {
	var sent []string
	impl := &recordingPlatformApp{clipboard: "clipboard text"}
	app := &App{
		options: Options{Title: "Native", Width: 640, Height: 480, NativeBridge: true},
		impl:    impl,
	}
	app.bridge = bridge.NewRouter(func(raw string) error {
		sent = append(sent, raw)
		return nil
	}, bridge.Limit{})
	app.registerNativeBridgeMethods()

	if err := app.bridge.Dispatch(`{"op":"req","id":"info","method":"gosx.desktop.app.info"}`); err != nil {
		t.Fatalf("dispatch app info: %v", err)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], `"nativeBridge":true`) {
		t.Fatalf("app info response = %#v, want nativeBridge true", sent)
	}

	if err := app.bridge.Dispatch(`{"op":"req","id":"title","method":"gosx.desktop.window.setTitle","payload":{"title":"Renamed"}}`); err != nil {
		t.Fatalf("dispatch set title: %v", err)
	}
	if impl.title != "Renamed" || app.options.Title != "Renamed" {
		t.Fatalf("title impl/app = %q/%q, want Renamed", impl.title, app.options.Title)
	}

	if err := app.bridge.Dispatch(`{"op":"req","id":"clip","method":"gosx.desktop.clipboard.writeText","payload":{"text":"copied"}}`); err != nil {
		t.Fatalf("dispatch clipboard write: %v", err)
	}
	if impl.clipboard != "copied" {
		t.Fatalf("clipboard = %q, want copied", impl.clipboard)
	}
}

func TestRunUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "windows" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		t.Skip("windows desktop backend is supported on this architecture")
	}
	err := Run(Options{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error = %v, want ErrUnsupported", err)
	}
}

type recordingPlatformApp struct {
	title     string
	clipboard string
}

func (a *recordingPlatformApp) Run() error                                       { return nil }
func (a *recordingPlatformApp) Close() error                                     { return nil }
func (a *recordingPlatformApp) Navigate(string) error                            { return nil }
func (a *recordingPlatformApp) SetHTML(string) error                             { return nil }
func (a *recordingPlatformApp) PostMessage(string) error                         { return nil }
func (a *recordingPlatformApp) ExecuteScript(string) error                       { return nil }
func (a *recordingPlatformApp) OpenDevTools() error                              { return nil }
func (a *recordingPlatformApp) PrependBootstrapScript(string) error              { return nil }
func (a *recordingPlatformApp) Minimize() error                                  { return nil }
func (a *recordingPlatformApp) Maximize() error                                  { return nil }
func (a *recordingPlatformApp) Restore() error                                   { return nil }
func (a *recordingPlatformApp) Focus() error                                     { return nil }
func (a *recordingPlatformApp) SetTitle(title string) error                      { a.title = title; return nil }
func (a *recordingPlatformApp) Serve(string, http.Handler) error                 { return nil }
func (a *recordingPlatformApp) OpenFileDialog(OpenFileOptions) ([]string, error) { return nil, nil }
func (a *recordingPlatformApp) SaveFileDialog(SaveFileOptions) (string, error)   { return "", nil }
func (a *recordingPlatformApp) Clipboard() (string, error)                       { return a.clipboard, nil }
func (a *recordingPlatformApp) SetClipboard(text string) error                   { a.clipboard = text; return nil }
func (a *recordingPlatformApp) OpenURL(string) error                             { return nil }
func (a *recordingPlatformApp) SetFullscreen(bool) error                         { return nil }
func (a *recordingPlatformApp) SetMinSize(int, int) error                        { return nil }
func (a *recordingPlatformApp) SetMaxSize(int, int) error                        { return nil }
func (a *recordingPlatformApp) NewWindow(WindowOptions) (*Window, error)         { return nil, ErrUnsupported }
func (a *recordingPlatformApp) RegisterProtocol(string) error                    { return nil }
func (a *recordingPlatformApp) RegisterFileType(string, string, string) error    { return nil }
func (a *recordingPlatformApp) SetMenuBar(Menu) error                            { return nil }
func (a *recordingPlatformApp) SetTray(TrayOptions) error                        { return nil }
func (a *recordingPlatformApp) CloseTray() error                                 { return nil }
func (a *recordingPlatformApp) Notify(Notification) error                        { return nil }
func (a *recordingPlatformApp) SetFileDropHandler(func([]string)) error          { return nil }

func TestNewUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "windows" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		t.Skip("windows desktop backend is supported on this architecture")
	}
	_, err := New(Options{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error = %v, want ErrUnsupported", err)
	}
}
