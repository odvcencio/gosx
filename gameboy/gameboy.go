// Package gameboy provides an optional cartridge engine for GoSX experiences.
// ROM bytes never enter the framework. Player and deployment cartridges are
// loaded locally, with battery saves and portable checkpoints on the device.
package gameboy

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"html"
	"io/fs"
	"net/http"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/server"
)

//go:embed assets
var assets embed.FS

// Props configures a framework-owned handheld, with no application script.
// CartridgeURL is an optional same-origin deployment manifest endpoint.
// Prefixes isolate local saves across experiences; blank uses framework defaults.
type Props struct {
	Heading          string `json:"-"`
	CartridgeURL     string `json:"cartridgeURL,omitempty"`
	BatteryPrefix    string `json:"batteryPrefix"`
	CheckpointPrefix string `json:"checkpointPrefix"`
	Protocol         string `json:"protocol"`
}

func assetURL(name string) string {
	data, _ := assets.ReadFile("assets/" + name)
	sum := sha256.Sum256(data)
	return "/gosx/gameboy/" + name + "?v=" + hex.EncodeToString(sum[:6])
}

// Assets serves the pinned driver and emulator. The Apache license and
// upstream provenance accompany the optional runtime; no game is embedded.
func Assets() http.Handler {
	root, _ := fs.Sub(assets, "assets")
	return http.StripPrefix("/gosx/gameboy/", http.FileServer(http.FS(root)))
}

// Install registers the optional asset pack before the framework's reserved
// /gosx/ runtime route. Use once on the app that renders Player.
func Install(app *server.App) {
	assets := Assets()
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/gosx/gameboy/") {
				if r.Method != http.MethodGet && r.Method != http.MethodHead {
					w.Header().Set("Allow", "GET, HEAD")
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
					return
				}
				assets.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
}

// Player authors one managed surface. The framework owns input, emulator
// timing, audio, checkpoint persistence, and teardown for this instance.
func Player(ctx *server.Context, props Props) gosx.Node {
	if props.Protocol == "" {
		props.Protocol = "gosx-handheld"
	}
	if props.BatteryPrefix == "" {
		props.BatteryPrefix = "gosx.gameboy.battery."
	}
	if props.CheckpointPrefix == "" {
		props.CheckpointPrefix = "gosx.gameboy.checkpoint.v1."
	}
	if props.Heading == "" {
		props.Heading = "POCKET HANDHELD"
	}
	ctx.AddHead(server.LifecycleScript(assetURL("runtime.js")), server.Stylesheet(assetURL("player.css")))
	raw, _ := json.Marshal(props)
	layout, _ := assets.ReadFile("assets/player.html")
	return ctx.Engine(engine.Config{Name: "GoSXGameBoy", Kind: engine.KindSurface, MountID: "gosx-gameboy", Capabilities: []engine.Capability{engine.CapCanvas, engine.CapWASM, engine.CapKeyboard, engine.CapPointer, engine.CapAudio, engine.CapStorage, engine.CapFetch}, Props: raw, MountAttrs: map[string]any{"class": "pocket"}}, gosx.RawHTML(strings.ReplaceAll(string(layout), "{{heading}}", html.EscapeString(props.Heading))))
}
