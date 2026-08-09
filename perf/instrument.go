package perf

import (
	"context"
	_ "embed"

	"github.com/chromedp/cdproto/page"
)

//go:embed instrument.js
var instrumentJS string

// Inject adds the GoSX performance instrumentation script to the browser
// context. The script runs before every page load (including navigations)
// via Page.addScriptToEvaluateOnNewDocument, so it wraps GoSX runtime
// exports the moment they become available.
//
// Call Inject once after creating the Driver, before the first Navigate.
func Inject(ctx context.Context) error {
	addScript := page.AddScriptToEvaluateOnNewDocument(instrumentJS)
	_, err := addScript.Do(ctx)
	return err
}

// InjectDriver is a convenience that calls Inject with the Driver's context.
func InjectDriver(d *Driver) error {
	return d.RunFunc(func(ctx context.Context) error {
		return Inject(ctx)
	})
}
