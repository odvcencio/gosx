# browsertest: browser checks from go test

| Call | Does |
|---|---|
| `Launch(t, Options{})` | A bound, active tab in a Chrome it starts (with `Start`, below), or in a running browser named by `Options.Endpoint`; closed when the test ends. Skips the test when no Chrome is found (`FindChrome`). |
| `Open(path, OpenOptions{...})` | Loads a file or URL with the viewport, touch, focus, offline and init scripts a check asks for. `CountFrames` installs `FrameCounter`. |
| `NewPage(t, tab, budget)` | `Run`, `Eval`, `Text`, `WaitFor`, `TryWait`, `Frames`, `WaitFrames`, `Screenshot`, with a deadline that `Stretch` and `MeasureFrameRate` move when the page draws slowly. |
| `Page.Step`, `Page.Shots` | Each `Run` has a deadline of its own (default `DefaultStep`, 60 s, times `Slow`, ending `StepMargin` before the page's deadline; `-1` for none). A failed `Run` or `WaitFor` names the action it was on (`Describe`), describes the page and saves a picture (see Failures). |

## Chrome startup

`Start(t.Context(), chromePath, flags...)` returns a browser whose empty tab is
already bound and active. Create navigation/assertion deadlines from `browser.Context`
afterward, and call `browser.Close()` in cleanup. Startup gets at most two
30-second attempts within a 65-second overall budget, reserving time for process,
pipe, and CDP cleanup. The WebSocket handshake uses the remaining attempt/overall
allowance. Startup deadlines and typed handshake timeouts can retry only while the
process is alive and the caller is not canceled. Process exits, invalid endpoints,
and other CDP errors fail immediately; assertions and page navigation are
never retried.

This package launches Chrome directly and uses chromedp's remote allocator for
the bound connection because chromedp v0.15.1's exec allocator has three relevant
behaviors that an option-only wrapper cannot fix:

- `WSURLReadTimeout` changes the default 20-second limit, but that endpoint wait
  does not select on caller cancellation.
- `readOutput` accumulates all pre-endpoint output in an unbounded buffer, even
  when `CombinedOutput` points to a bounded writer.
- The tail copy starts only after the endpoint is found, making output-drain
  ownership and failed-startup cleanup hard to join from a wrapper.

The helper continuously drains an owned pipe into an 8 KiB tail and parses only
bounded endpoint lines. It observes process exit independently of pipe draining,
so a descendant holding stdout open cannot turn an immediate fatal exit into a
retryable timeout. Failed attempts join the initial CDP run, allocator, process,
and drain before removing the fresh profile. On Linux, cancellation kills the
owned process group, including launcher/Chrome descendants. Other platforms use
the standard process kill and a bounded pipe-drain close; descendant-group cleanup
is Linux-specific. The flags preserve chromedp v0.15.1's defaults and each caller's
rendering/codec settings. Product, visual, and performance launchers do not use
this test helper.

The remote allocator opens a new test tab beside Chrome's initial blank tab.
Chrome can publish DevTools before its headless window accepts new tabs. Startup
waits for the specific typed `Target.createTarget` response "no browser is open"
on the same connection, within the existing attempt and overall deadlines.
Other target errors remain fatal. Startup activates the tab before returning,
preserving the exec allocator's visible-page behavior for requestAnimationFrame and chromedp's default polling.

Diagnostics remove terminal controls, URLs, profile paths, endpoint addresses,
and common credential fields. A truncated first line is discarded so redaction
does not start in the middle of a secret value. This is diagnostic hygiene for
local test processes, not a general-purpose arbitrary-log sanitizer.

Fake executable and CDP peer tests cover fatal exits, inherited descendant pipes,
timeout-only retries, fresh profiles, complete pipe floods, prompt cancellation
before and during CDP bind, overall budgets, and successful browser lifetime after
startup. These run in the ordinary and focused race CI partitions. POSIX process
fixtures run on Linux/macOS; portable endpoint/diagnostic tests also compile and
run on Windows. Set `GOSX_CHROME_BIN` to run the three-launch real Chrome smoke,
including visible-page and animation-frame polling checks.

## Failures

A failed `Run` or `WaitFor` ends the test with four things:

1. the stage and, for `Run`, the action it was on: `press snap (action 2 of 3, query [data-control=snap]): not done within the step limit of 1m42s: context deadline exceeded`;
2. `page:`, the check's own `Snapshot`, when it set one;
3. `look:`, the page's ready state, visibility, hash, focused element and frame count, and for a query what its selector matches: how many, whether the first is shown and enabled, its size and place, and the element over its centre when another one covers it;
4. `picture:`, the path of a PNG of the viewport, in `Page.Shots`, else `BROWSERTEST_SHOTS`, else `browsertest-failures` in the temporary folder.

The look and the picture run on a context of their own with a 15-second limit, so they work after the page's deadline has cancelled `Page.Ctx`. A step that sticks fails at its own limit, at least `StepMargin` before the page's deadline, so a `Snapshot` that uses `Page.Ctx` still answers.
