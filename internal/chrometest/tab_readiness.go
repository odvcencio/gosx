package chrometest

import (
	"context"
	"errors"
	"time"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// Chrome can publish DevTools before its headless window accepts a new tab.
// Wait on that specific readiness response within the caller's startup budget;
// other protocol failures and all navigation/assertion failures remain fatal.
func bindInitialTab(ctx context.Context) error {
	for {
		err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			state := chromedp.FromContext(ctx)
			return target.ActivateTarget(state.Target.TargetID).Do(cdp.WithExecutor(ctx, state.Browser))
		}))
		var protocolError *cdproto.Error
		if !errors.As(err, &protocolError) || protocolError.Code != -32000 ||
			protocolError.Message != "Failed to open new tab - no browser is open" || chromedp.FromContext(ctx).Target != nil {
			return err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			stopTimer(timer)
			return ctx.Err()
		case <-timer.C:
		}
	}
}
