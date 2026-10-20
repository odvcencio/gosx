package main

import (
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"time"
)

// readyPrefix starts the line the engine prints once it listens. The desktop
// host waits for it (sidecar.Options.ReadyLine) before opening the page.
const readyPrefix = "desktop-app engine: "

// serveEngine is the sidecar: a loopback HTTP server that stands in for a
// game server, audio engine, or language server. It runs in its own process,
// so a crash here does not take the window down.
func serveEngine() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	started := time.Now()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page(os.Getpid()))
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"pid":%d,"uptimeMs":%d}`, os.Getpid(), time.Since(started).Milliseconds())
	})
	fmt.Printf("%shttp://%s/\n", readyPrefix, ln.Addr())
	return http.Serve(ln, mux)
}

func page(pid int) string {
	return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>GoSX desktop app</title>
<style>
body{margin:0;padding:clamp(16px,4vw,32px);background:#10151c;color:#dde6f0;font:16px/1.5 "Segoe UI",system-ui,sans-serif}
main{max-width:720px;margin:0 auto}
p.actions{display:flex;flex-wrap:wrap;gap:8px}
button{font:inherit;min-height:44px;padding:6px 14px}
pre{background:#18202a;padding:12px;border-radius:6px;white-space:pre-wrap}
</style></head><body><main>
<h1>GoSX desktop app</h1>
<p>This page comes from the engine sidecar (process ` + html.EscapeString(fmt.Sprint(pid)) + `).
Go services are called through <code>window.gosxDesktop</code>.</p>
<p class=actions><button id=open>Open a text file</button><button id=status>Engine status</button></p>
<pre id=out>Ready.</pre>
<script>
const out = document.getElementById('out');
const show = value => { out.textContent = typeof value === 'string' ? value : JSON.stringify(value, null, 2); };
const desktop = () => window.gosxDesktop;
document.getElementById('open').onclick = async () => {
  try { show(await desktop().service('files').open()); } catch (e) { show('open failed: ' + e.message); }
};
document.getElementById('status').onclick = async () => show(await (await fetch('/api/status')).json());
// Host events: menu picks.
window.chrome?.webview?.addEventListener('message', event => {
  try {
    const message = JSON.parse(event.data);
    if (message.op === 'evt' && message.method === 'app.fileOpened') show(message.payload);
  } catch (_) {}
});
</script></main></body></html>`
}
