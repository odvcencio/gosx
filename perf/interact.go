package perf

import (
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

// Click clicks the first element matching selector and waits for idle.
func Click(d *Driver, selector string) error {
	return chromedp.Run(d.ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Click(selector, chromedp.ByQuery),
		chromedp.Sleep(100*time.Millisecond),
	)
}

// Type types text into the element matching selector.
func Type(d *Driver, selector, text string) error {
	return chromedp.Run(d.ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.SendKeys(selector, text, chromedp.ByQuery),
		chromedp.Sleep(100*time.Millisecond),
	)
}

// Scroll scrolls the page by the given number of pixels.
func Scroll(d *Driver, pixels int) error {
	js := fmt.Sprintf("window.scrollBy(0, %d)", pixels)
	return d.Evaluate(js, nil)
}

// WaitForMark polls for a performance mark by name, timing out after dur.
func WaitForMark(d *Driver, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	js := fmt.Sprintf(`performance.getEntriesByName(%q).length > 0`, name)
	for time.Now().Before(deadline) {
		var found bool
		if err := d.Evaluate(js, &found); err != nil {
			return err
		}
		if found {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for performance mark %q", name)
}

// WaitIdle waits for a brief idle period (RAF + microtask flush).
func WaitIdle(d *Driver) error {
	return d.Evaluate(`new Promise(r => setTimeout(r, 100))`, nil)
}
