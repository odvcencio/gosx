package perf

import (
	"fmt"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// MobileProfile describes a device simulation preset. Matches the fields
// Chrome DevTools' device toolbar uses.
type MobileProfile struct {
	Name        string
	Width       int64
	Height      int64
	ScaleFactor float64
	UserAgent   string
}

// Pixel7 approximates a mid-range Android phone (what the user actually
// ships to). Numbers match Chrome DevTools' built-in "Pixel 7" preset.
var Pixel7 = MobileProfile{
	Name:        "Pixel 7",
	Width:       412,
	Height:      915,
	ScaleFactor: 2.625,
	UserAgent:   "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
}

// iPhone14 approximates an iPhone 14 for parity testing. Safari UA.
var iPhone14 = MobileProfile{
	Name:        "iPhone 14",
	Width:       390,
	Height:      844,
	ScaleFactor: 3,
	UserAgent:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
}

// ApplyCPUThrottle sets CPU throttling so subsequent work runs at 1/rate
// the speed of the real CPU. rate=1 means no throttling; rate=4 means
// roughly a mid-range 2020 phone. Chrome DevTools' "4x slowdown" preset
// matches rate=4.
//
// Must be called AFTER New() but BEFORE Navigate() for the throttle to
// cover the initial page load.
func ApplyCPUThrottle(d *Driver, rate float64) error {
	if d == nil || rate <= 1 {
		return nil
	}
	return chromedp.Run(d.ctx, emulation.SetCPUThrottlingRate(rate))
}

// ApplyMobileEmulation configures viewport, device scale, and user agent
// to match the given MobileProfile. Scene3D (and anything that reads
// capability.coarsePointer / hover) will see itself as running on a
// touch device with no hover.
func ApplyMobileEmulation(d *Driver, profile MobileProfile) error {
	if d == nil || profile.Width == 0 {
		return nil
	}
	metrics := emulation.SetDeviceMetricsOverride(
		profile.Width,
		profile.Height,
		profile.ScaleFactor,
		true, // mobile
	)
	if err := chromedp.Run(d.ctx, metrics); err != nil {
		return fmt.Errorf("emulation.SetDeviceMetricsOverride: %w", err)
	}
	if profile.UserAgent != "" {
		ua := emulation.SetUserAgentOverride(profile.UserAgent).WithPlatform("Android")
		if err := chromedp.Run(d.ctx, ua); err != nil {
			return fmt.Errorf("emulation.SetUserAgentOverride: %w", err)
		}
	}
	return nil
}

// ResolveMobileProfile maps a shorthand name to a MobileProfile. Used by
// the --mobile CLI flag. Unknown names return the empty profile.
func ResolveMobileProfile(name string) MobileProfile {
	switch name {
	case "pixel7", "pixel", "android":
		return Pixel7
	case "iphone", "iphone14", "ios":
		return iPhone14
	}
	return MobileProfile{}
}
