package desktop

import (
	"errors"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx/desktop/bridge"
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

func TestNormalizeOptionsKeepsWebView2ShippingOptions(t *testing.T) {
	options, err := normalizeOptions(Options{
		BrowserExecutableFolder:    `C:\runtime\fixed`,
		AdditionalBrowserArguments: "--mute-audio --autoplay-policy=no-user-gesture-required",
	})
	if err != nil {
		t.Fatalf("normalize options: %v", err)
	}
	if options.BrowserExecutableFolder != `C:\runtime\fixed` {
		t.Fatalf("browser executable folder = %q", options.BrowserExecutableFolder)
	}
	if options.AdditionalBrowserArguments != "--mute-audio --autoplay-policy=no-user-gesture-required" {
		t.Fatalf("additional browser arguments = %q", options.AdditionalBrowserArguments)
	}
}

func TestNormalizeOptionsRejectsNULInWebView2Options(t *testing.T) {
	for name, options := range map[string]Options{
		"browser executable folder":    {BrowserExecutableFolder: "bad\x00folder"},
		"additional browser arguments": {AdditionalBrowserArguments: "--flag\x00value"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeOptions(options); !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("error = %v, want ErrInvalidOptions", err)
			}
		})
	}
}

func TestDesktopWebView2AvailabilityErrorsAreDistinctAndWrapped(t *testing.T) {
	loaderErr := webView2LoaderUnavailable(errors.New("loader not found"))
	if !errors.Is(loaderErr, ErrWebView2LoaderUnavailable) || !errors.Is(loaderErr, ErrWebView2Unavailable) {
		t.Fatalf("loader error = %v, want loader and general WebView2 sentinels", loaderErr)
	}
	runtimeErr := webView2RuntimeUnavailable(errors.New("no runtime"))
	if !errors.Is(runtimeErr, ErrWebView2RuntimeUnavailable) || !errors.Is(runtimeErr, ErrWebView2Unavailable) {
		t.Fatalf("runtime error = %v, want runtime and general WebView2 sentinels", runtimeErr)
	}
	if errors.Is(loaderErr, ErrWebView2RuntimeUnavailable) || errors.Is(runtimeErr, ErrWebView2LoaderUnavailable) {
		t.Fatalf("loader and runtime errors must remain distinct: loader=%v runtime=%v", loaderErr, runtimeErr)
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

func TestDesktopAppReloadDelegatesToPlatform(t *testing.T) {
	impl := &recordingPlatformApp{}
	app := &App{impl: impl}
	if err := app.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if impl.reloads != 1 {
		t.Fatalf("reload calls = %d, want 1", impl.reloads)
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
	reloads   int
}

func (a *recordingPlatformApp) Run() error                                       { return nil }
func (a *recordingPlatformApp) Close() error                                     { return nil }
func (a *recordingPlatformApp) Reload() error                                    { a.reloads++; return nil }
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

func TestComposeBrowserArguments(t *testing.T) {
	tests := []struct {
		name     string
		options  Options
		operator string
		want     string
	}{
		{name: "empty", want: ""},
		{name: "app only", options: Options{AdditionalBrowserArguments: " --no-first-run "}, want: "--no-first-run"},
		{name: "operator kept last", options: Options{AdditionalBrowserArguments: "--no-first-run"}, operator: "--use-angle=d3d11", want: "--no-first-run --use-angle=d3d11"},
		{name: "operator only", operator: "--use-angle=d3d11", want: "--use-angle=d3d11"},
		{name: "high performance", options: Options{GPU: GPUOptions{Preference: GPUPreferenceHighPerformance}}, want: "--force_high_performance_gpu"},
		{name: "low power", options: Options{GPU: GPUOptions{Preference: GPUPreferenceLowPower}}, want: "--force_low_power_gpu"},
		{name: "adapter wins over preference", options: Options{GPU: GPUOptions{Preference: GPUPreferenceLowPower, AdapterLUID: GPUAdapterLUID{High: 0, Low: 81115}}}, want: "--use-adapter-luid=0,81115"},
		{name: "negative high part", options: Options{GPU: GPUOptions{AdapterLUID: GPUAdapterLUID{High: -1, Low: 4294967295}}}, want: "--use-adapter-luid=-1,4294967295"},
		{name: "operator GPU switch drops app GPU switch", options: Options{AdditionalBrowserArguments: "--no-first-run", GPU: GPUOptions{Preference: GPUPreferenceHighPerformance}}, operator: "--force_low_power_gpu --use-adapter-luid=0,90433", want: "--no-first-run --force_low_power_gpu --use-adapter-luid=0,90433"},
		{name: "mute audio", options: Options{MuteAudio: true}, want: "--mute-audio"},
		{name: "mute audio not repeated", options: Options{MuteAudio: true, AdditionalBrowserArguments: "--mute-audio"}, want: "--mute-audio"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := composeBrowserArguments(tt.options, tt.operator); got != tt.want {
				t.Fatalf("composeBrowserArguments() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewRejectsUnknownGPUPreference(t *testing.T) {
	_, err := normalizeOptions(Options{GPU: GPUOptions{Preference: "fastest"}})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("normalizeOptions error = %v, want ErrInvalidOptions", err)
	}
	options, err := normalizeOptions(Options{GPU: GPUOptions{Preference: GPUPreferenceHighPerformance}})
	if err != nil || options.GPU.Preference != GPUPreferenceHighPerformance {
		t.Fatalf("normalizeOptions = %+v, %v", options.GPU, err)
	}
}

func TestSetBrowserArgumentsEnvReplacesEarlierValue(t *testing.T) {
	t.Setenv(webView2BrowserArgumentsEnv, "--from-an-earlier-app")
	if err := setBrowserArgumentsEnv(""); err != nil {
		t.Fatal(err)
	}
	if value, ok := os.LookupEnv(webView2BrowserArgumentsEnv); ok {
		t.Fatalf("empty composition left %q in the environment", value)
	}
	if err := setBrowserArgumentsEnv("--force_low_power_gpu"); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(webView2BrowserArgumentsEnv); got != "--force_low_power_gpu" {
		t.Fatalf("environment = %q", got)
	}
}

func TestParseBackgroundColor(t *testing.T) {
	tests := []struct {
		in        string
		canonical string
		color     rgbColor
		set       bool
		wantErr   bool
	}{
		{in: "", set: false},
		{in: "#131007", canonical: "#131007", color: rgbColor{0x13, 0x10, 0x07}, set: true},
		{in: " #ABCDEF ", canonical: "#abcdef", color: rgbColor{0xab, 0xcd, 0xef}, set: true},
		{in: "#fff", canonical: "#ffffff", color: rgbColor{0xff, 0xff, 0xff}, set: true},
		{in: "131007", wantErr: true},
		{in: "#12345", wantErr: true},
		{in: "#gggggg", wantErr: true},
		{in: "#1234567", wantErr: true},
	}
	for _, tt := range tests {
		color, canonical, set, err := parseBackgroundColor(tt.in)
		if tt.wantErr {
			if !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("parseBackgroundColor(%q) error = %v, want ErrInvalidOptions", tt.in, err)
			}
			continue
		}
		if err != nil || color != tt.color || canonical != tt.canonical || set != tt.set {
			t.Fatalf("parseBackgroundColor(%q) = %+v, %q, %v, %v", tt.in, color, canonical, set, err)
		}
	}
	options, err := normalizeOptions(Options{BackgroundColor: "#ABC"})
	if err != nil || options.BackgroundColor != "#aabbcc" {
		t.Fatalf("normalizeOptions background = %q, %v", options.BackgroundColor, err)
	}
	if _, err := normalizeOptions(Options{BackgroundColor: "dark"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("normalizeOptions(dark) error = %v", err)
	}
}

func TestStartupTimelineWithoutReporter(t *testing.T) {
	var nilApp *App
	if got := nilApp.StartupTimeline(); got != (StartupTimeline{}) {
		t.Fatalf("nil app timeline = %+v", got)
	}
	if got := (&App{}).StartupTimeline(); got != (StartupTimeline{}) {
		t.Fatalf("app without backend timeline = %+v", got)
	}
}
