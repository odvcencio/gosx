// Package browsertest runs browser checks from go test: it starts Chrome
// with a bounded, retried startup (Start, Launch), opens a page with the
// emulation a check needs (Open), and drives it with waits that stretch when
// the page draws slowly (Page).
//
// A check of a packaged page reads like this:
//
//	tab := browsertest.Launch(t, browsertest.Options{})
//	p := browsertest.NewPage(t, tab, 100*time.Second)
//	p.Run("open", browsertest.Open(html, browsertest.OpenOptions{Width: 1024, Height: 768, Offline: true, CountFrames: true})...)
//	p.WaitFor("ready", `document.documentElement.dataset.engine === "ready"`, 40*time.Second)
//
// It is test infrastructure, not part of the GoSX runtime or its browser
// assets. Product, visual and performance launchers use package perf.
package browsertest
