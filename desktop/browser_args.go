package desktop

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// webView2BrowserArgumentsEnv is the process-wide variable WebView2 reads
// when it creates a browser environment.
const webView2BrowserArgumentsEnv = "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"

// GPUPreference selects which GPU class the WebView2 GPU process asks for on
// machines with more than one adapter, such as laptops with integrated and
// discrete graphics.
type GPUPreference string

const (
	// GPUPreferenceDefault leaves the choice to Chromium and Windows.
	GPUPreferenceDefault GPUPreference = ""
	// GPUPreferenceHighPerformance asks for the discrete GPU
	// (--force_high_performance_gpu).
	GPUPreferenceHighPerformance GPUPreference = "high-performance"
	// GPUPreferenceLowPower asks for the integrated GPU
	// (--force_low_power_gpu).
	GPUPreferenceLowPower GPUPreference = "low-power"
)

// GPUAdapterLUID identifies one DXGI adapter by its locally unique ID, as
// reported by IDXGIAdapter1::GetDesc1. The zero value means "not set"; no
// real adapter has the LUID 0:0.
type GPUAdapterLUID struct {
	High int32
	Low  uint32
}

// IsZero reports whether the LUID is unset.
func (l GPUAdapterLUID) IsZero() bool { return l.High == 0 && l.Low == 0 }

// GPUAdapter describes one hardware graphics adapter reported by GPUAdapters.
type GPUAdapter struct {
	Name                 string
	VendorID             uint32
	DeviceID             uint32
	DedicatedVideoMemory uint64
	LUID                 GPUAdapterLUID
}

// GPUOptions selects the adapter WebView2 renders on. The values become
// Chromium command-line switches, so they share the process-wide scope of
// Options.AdditionalBrowserArguments.
type GPUOptions struct {
	// Preference asks for the high-performance or low-power adapter.
	Preference GPUPreference
	// AdapterLUID selects one adapter exactly (--use-adapter-luid). When
	// set, it takes precedence over Preference.
	AdapterLUID GPUAdapterLUID
}

// gpuSwitchNames are the Chromium switches that pick a GPU. When the operator
// environment names any of them, the app's GPU switches are dropped so a
// field or test override always wins.
var gpuSwitchNames = []string{
	"--force_high_performance_gpu",
	"--force_low_power_gpu",
	"--use-adapter-luid",
}

var (
	operatorBrowserArgumentsOnce  sync.Once
	operatorBrowserArgumentsValue string
)

// operatorBrowserArguments returns the value of
// WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS as it was the first time any desktop
// app in this process built its browser arguments. Taking the snapshot once
// keeps a second environment (reload, new window) from reading back the
// merged value this package wrote.
func operatorBrowserArguments() string {
	operatorBrowserArgumentsOnce.Do(func() {
		operatorBrowserArgumentsValue = os.Getenv(webView2BrowserArgumentsEnv)
	})
	return operatorBrowserArgumentsValue
}

func normalizeGPUOptions(gpu GPUOptions) (GPUOptions, error) {
	switch gpu.Preference {
	case GPUPreferenceDefault, GPUPreferenceHighPerformance, GPUPreferenceLowPower:
	default:
		return GPUOptions{}, fmt.Errorf("%w: unsupported GPU preference %q", ErrInvalidOptions, gpu.Preference)
	}
	return gpu, nil
}

// gpuBrowserArguments converts GPU options to Chromium switches.
func gpuBrowserArguments(gpu GPUOptions) []string {
	if !gpu.AdapterLUID.IsZero() {
		return []string{"--use-adapter-luid=" +
			strconv.FormatInt(int64(gpu.AdapterLUID.High), 10) + "," +
			strconv.FormatUint(uint64(gpu.AdapterLUID.Low), 10)}
	}
	switch gpu.Preference {
	case GPUPreferenceHighPerformance:
		return []string{"--force_high_performance_gpu"}
	case GPUPreferenceLowPower:
		return []string{"--force_low_power_gpu"}
	}
	return nil
}

// composeBrowserArguments builds the WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS
// value for an app. The order is: the app's AdditionalBrowserArguments, the
// app's GPU switches, --mute-audio when MuteAudio is set, and last the
// operator's pre-existing environment value. Chromium keeps the last value of
// a repeated switch, so the operator's value wins; GPU switches are dropped
// entirely when the operator names any GPU switch, because
// --force_high_performance_gpu and --force_low_power_gpu are separate
// switches rather than two values of one.
func composeBrowserArguments(options Options, operator string) string {
	parts := make([]string, 0, 4)
	if app := strings.TrimSpace(options.AdditionalBrowserArguments); app != "" {
		parts = append(parts, app)
	}
	operator = strings.TrimSpace(operator)
	if !mentionsSwitch(operator, gpuSwitchNames) {
		parts = append(parts, gpuBrowserArguments(options.GPU)...)
	}
	if options.MuteAudio && !mentionsSwitch(strings.Join(parts, " "), []string{"--mute-audio"}) {
		parts = append(parts, "--mute-audio")
	}
	if operator != "" {
		parts = append(parts, operator)
	}
	return strings.Join(parts, " ")
}

// setBrowserArgumentsEnv makes WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS equal
// args, removing the variable when args is empty.
func setBrowserArgumentsEnv(args string) error {
	if args == "" {
		if err := os.Unsetenv(webView2BrowserArgumentsEnv); err != nil {
			return fmt.Errorf("unset %s: %w", webView2BrowserArgumentsEnv, err)
		}
		return nil
	}
	if err := os.Setenv(webView2BrowserArgumentsEnv, args); err != nil {
		return fmt.Errorf("set %s: %w", webView2BrowserArgumentsEnv, err)
	}
	return nil
}

// mentionsSwitch reports whether args contains one of the named switches,
// with or without a value.
func mentionsSwitch(args string, names []string) bool {
	for _, field := range strings.Fields(args) {
		name, _, _ := strings.Cut(field, "=")
		for _, want := range names {
			if name == want {
				return true
			}
		}
	}
	return false
}
