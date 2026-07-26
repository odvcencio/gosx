package route

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/server"
)

func TestDefaultFileRendererMapsVideoPersistenceAndLockLiteralAttrs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.gsx")
	source := `package docs

func Page() Node {
	return <Video
		src="media/promo.mp4"
		persistPrefs
		persistKey="  channel-42  "
		lockInput
	/>
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := &RouteContext{}
	node, err := DefaultFileRenderer(ctx, FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}

	html := gosx.RenderHTML(node)
	for _, snippet := range []string{
		`data-gosx-engine="GoSXVideo"`,
		`<video data-gosx-video-fallback="true" src="/media/promo.mp4"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in rendered video html %q", snippet, html)
		}
	}
	for _, snippet := range []string{
		`persistPrefs`,
		`persistKey`,
		`lockInput`,
	} {
		if strings.Contains(html, snippet) {
			t.Fatalf("did not expect video engine prop %q to leak into fallback html %q", snippet, html)
		}
	}

	head := gosx.RenderHTML(ctx.Runtime().Head())
	for _, snippet := range []string{
		`"persistPrefs": true`,
		`"persistKey": "channel-42"`,
		`"lockInput": true`,
	} {
		if !strings.Contains(head, snippet) {
			t.Fatalf("expected %q in video runtime head %q", snippet, head)
		}
	}
}

func TestFileProgramRendererMapsVideoPersistenceLockAndSyncTuningExpressions(t *testing.T) {
	node := &ir.Node{
		Kind: ir.NodeComponent,
		Tag:  "Video",
		Attrs: []ir.Attr{
			{Kind: ir.AttrStatic, Name: "src", Value: "media/live.mp4"},
			{Kind: ir.AttrExpr, Name: "persist_prefs", Expr: "prefsEnabled"},
			{Kind: ir.AttrExpr, Name: "persist_key", Expr: "persistKey"},
			{Kind: ir.AttrExpr, Name: "lock_input", Expr: "lockInput"},
			{Kind: ir.AttrExpr, Name: "sync_tuning", Expr: "syncTuning"},
		},
	}

	var cfg engine.Config
	env := fileRenderEnv{
		values: map[string]any{
			"prefsEnabled": true,
			"persistKey":   "live-room",
			"lockInput":    true,
			"syncTuning": server.SyncTuning{
				ToleranceThreshold: 0.08,
				SeekThreshold:      1.75,
				HysteresisCount:    3,
				WarmupMs:           250,
			},
		},
		renderEngine: func(next engine.Config, fallback gosx.Node) gosx.Node {
			cfg = next
			return fallback
		},
	}

	html := (&fileProgramRenderer{}).renderVideo(node, env)
	if !strings.Contains(html, `<video data-gosx-video-fallback="true" src="/media/live.mp4"`) {
		t.Fatalf("expected normalized expression src in rendered video html %q", html)
	}

	var props map[string]any
	if err := json.Unmarshal(cfg.Props, &props); err != nil {
		t.Fatalf("unmarshal video engine props: %v", err)
	}
	data, err := json.MarshalIndent(props, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	renderedProps := string(data)
	for _, snippet := range []string{
		`"persistPrefs": true`,
		`"persistKey": "live-room"`,
		`"lockInput": true`,
		`"syncTuning": {`,
		`"toleranceThreshold": 0.08`,
		`"seekThreshold": 1.75`,
		`"hysteresisCount": 3`,
		`"warmupMs": 250`,
	} {
		if !strings.Contains(renderedProps, snippet) {
			t.Fatalf("expected %q in video engine props %q", snippet, renderedProps)
		}
	}
}

func TestFileProgramRendererConsumesExportedVideoPropAliasesFromSpread(t *testing.T) {
	node := &ir.Node{
		Kind: ir.NodeComponent,
		Tag:  "Video",
		Attrs: []ir.Attr{
			{Kind: ir.AttrSpread, Expr: "video"},
		},
	}

	var cfg engine.Config
	env := fileRenderEnv{
		values: map[string]any{
			"video": map[string]any{
				"Src":          "media/spread.mp4",
				"PersistPrefs": true,
				"PersistKey":   "spread-room",
				"LockInput":    true,
				"SyncTuning": server.SyncTuning{
					RateThreshold: 0.18,
					RateHoldMs:    400,
				},
				"data-testid": "video-fallback",
			},
		},
		renderEngine: func(next engine.Config, fallback gosx.Node) gosx.Node {
			cfg = next
			return fallback
		},
	}

	html := (&fileProgramRenderer{}).renderVideo(node, env)
	for _, snippet := range []string{
		`src="/media/spread.mp4"`,
		`data-testid="video-fallback"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in rendered spread video html %q", snippet, html)
		}
	}
	for _, leaked := range []string{
		`PersistPrefs`,
		`PersistKey`,
		`LockInput`,
		`SyncTuning`,
	} {
		if strings.Contains(html, leaked) {
			t.Fatalf("did not expect exported video prop alias %q to leak into fallback html %q", leaked, html)
		}
	}

	var props map[string]any
	if err := json.Unmarshal(cfg.Props, &props); err != nil {
		t.Fatalf("unmarshal video engine props: %v", err)
	}
	data, err := json.MarshalIndent(props, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	renderedProps := string(data)
	for _, snippet := range []string{
		`"persistPrefs": true`,
		`"persistKey": "spread-room"`,
		`"lockInput": true`,
		`"syncTuning": {`,
		`"rateThreshold": 0.18`,
		`"rateHoldMs": 400`,
	} {
		if !strings.Contains(renderedProps, snippet) {
			t.Fatalf("expected %q in video engine props %q", snippet, renderedProps)
		}
	}
}

func TestFileProgramRendererMapsVideoParityOptions(t *testing.T) {
	node := &ir.Node{
		Kind: ir.NodeComponent,
		Tag:  "Video",
		Attrs: []ir.Attr{
			{Kind: ir.AttrStatic, Name: "src", Value: "media/live.m3u8"},
			{Kind: ir.AttrExpr, Name: "subtitles", Expr: "subtitles"},
			{Kind: ir.AttrExpr, Name: "audio_source", Expr: "audioSource"},
			{Kind: ir.AttrExpr, Name: "fullscreen", Expr: "fullscreen"},
			{Kind: ir.AttrExpr, Name: "telemetry", Expr: "telemetry"},
		},
	}

	var cfg engine.Config
	env := fileRenderEnv{
		values: map[string]any{
			"subtitles": server.SubtitleOptions{
				OffsetMs:            -125,
				Scale:               "m",
				Style:               "minimal",
				GapBridgeMs:         650,
				BitmapPrefetchLimit: 4,
				RefreshEndpoint:     "/api/subtitle/refresh",
			},
			"audioSource": server.AudioSourceOptions{QueryParam: "audio"},
			"fullscreen":  server.FullscreenOptions{Target: "video"},
			"telemetry": server.VideoTelemetryOptions{
				Endpoint:              "/api/playback/telemetry",
				StallRecoveryDelayMs:  5000,
				MaxStallRecoveryCount: 1,
			},
		},
		renderEngine: func(next engine.Config, fallback gosx.Node) gosx.Node {
			cfg = next
			return fallback
		},
	}

	html := (&fileProgramRenderer{}).renderVideo(node, env)
	for _, leaked := range []string{`subtitles`, `audio_source`, `fullscreen`, `telemetry`} {
		if strings.Contains(html, leaked) {
			t.Fatalf("did not expect video prop %q to leak into fallback html %q", leaked, html)
		}
	}

	var props map[string]any
	if err := json.Unmarshal(cfg.Props, &props); err != nil {
		t.Fatalf("unmarshal video engine props: %v", err)
	}
	data, err := json.MarshalIndent(props, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	renderedProps := string(data)
	for _, snippet := range []string{
		`"subtitles": {`,
		`"offsetMs": -125`,
		`"scale": "m"`,
		`"style": "minimal"`,
		`"gapBridgeMs": 650`,
		`"bitmapPrefetchLimit": 4`,
		`"refreshEndpoint": "/api/subtitle/refresh"`,
		`"audioSource": {`,
		`"queryParam": "audio"`,
		`"fullscreen": {`,
		`"target": "video"`,
		`"telemetry": {`,
		`"endpoint": "/api/playback/telemetry"`,
		`"stallRecoveryDelayMs": 5000`,
		`"maxStallRecoveryCount": 1`,
	} {
		if !strings.Contains(renderedProps, snippet) {
			t.Fatalf("expected %q in video engine props %q", snippet, renderedProps)
		}
	}
}
