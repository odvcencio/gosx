# Chrome startup for tests

`Start(t.Context(), chromePath, flags...)` returns a browser whose empty tab is
already bound and active. Create navigation/assertion deadlines from `browser.Context`
afterward, and call `browser.Close()` in cleanup. Startup gets at most two
30-second attempts within a 65-second overall budget, reserving time for process,
pipe, and CDP cleanup. The WebSocket handshake uses the remaining attempt/overall
allowance. Startup deadlines and typed handshake timeouts can retry only while the
process is alive and the caller is not canceled. Process exits, invalid endpoints,
and non-timeout CDP errors fail immediately; assertions and page navigation are
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
Startup activates that tab before returning, preserving the exec allocator's
visible-page behavior for requestAnimationFrame and chromedp's default polling.

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
