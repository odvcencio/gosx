//go:build js && wasm && !gosx_tiny_islands_only

// Video-sync JS bridge — WASM runtime exports.
//
// Registers the __gosx_video_sync_* entry points that let JS drive the
// per-mount videosync.Engine instances held by Bridge. The hot-path Tick
// export returns positional numerics (a JS array of 8 floats) so that the
// rAF callback can destructure without any JSON parsing overhead. Cold paths
// (new, stats, last_reason) may return strings.
//
// Pairs with video_sync_islands_only.go, which is the no-op stub used by
// the tiny build so the registerVideoSyncRuntime symbol always exists.

package main

import (
	"syscall/js"

	"m31labs.dev/gosx/client/bridge"
)

func registerVideoSyncRuntime(b *bridge.Bridge) {
	setRuntimeFunc("__gosx_video_sync_new", videoSyncNewFunc(b))
	setRuntimeFunc("__gosx_video_sync_ingest", videoSyncIngestFunc(b))
	setRuntimeFunc("__gosx_video_sync_rtt", videoSyncRTTFunc(b))
	setRuntimeFunc("__gosx_video_sync_tick", videoSyncTickFunc(b))
	setRuntimeFunc("__gosx_video_sync_playback_start", videoSyncPlaybackStartFunc(b))
	setRuntimeFunc("__gosx_video_sync_dispose", videoSyncDisposeFunc(b))
	setRuntimeFunc("__gosx_video_sync_stats", videoSyncStatsFunc(b))
	setRuntimeFunc("__gosx_video_sync_last_reason", videoSyncLastReasonFunc(b))
}

// videoSyncNewFunc registers a new videosync.Engine under the given id.
//
// Call shape:
//
//	__gosx_video_sync_new(id, configJSON)
//
// configJSON may be "" or an empty string to use DefaultConfig.
// Returns null on success, or an "error: …" string on failure.
func videoSyncNewFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			return js.Null()
		}
		if err := b.NewVideoSync(args[0].String(), args[1].String()); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

// videoSyncIngestFunc delivers a server heartbeat to the engine under id.
//
// Call shape:
//
//	__gosx_video_sync_ingest(id, serverTimeMs, position, playing, recvPerfMs)
func videoSyncIngestFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 5 {
			return js.Null()
		}
		b.IngestVideoSync(
			args[0].String(),
			uint64(args[1].Float()),
			float32(args[2].Float()),
			args[3].Bool(),
			args[4].Float(),
		)
		return js.Null()
	})
}

// videoSyncRTTFunc records a round-trip-time sample (ms) for the engine under id.
//
// Call shape:
//
//	__gosx_video_sync_rtt(id, rttMs)
func videoSyncRTTFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			return js.Null()
		}
		b.RTTVideoSync(args[0].String(), args[1].Float())
		return js.Null()
	})
}

// videoSyncTickFunc evaluates one frame of drift correction.
//
// Returns positional numerics — a JS array of 8 floats — so the rAF
// callback can destructure without JSON parsing overhead:
//
//	[kind, rate, seekTo, ready, stalled, actualRate, preloadPhase, resetRate]
//
// Call shape:
//
//	__gosx_video_sync_tick(id, currentTime, perfNowMs, bufferedAhead, paused)
func videoSyncTickFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 5 {
			return js.Null()
		}
		d := b.TickVideoSync(
			args[0].String(),
			args[1].Float(),
			args[2].Float(),
			args[3].Float(),
			args[4].Bool(),
		)
		return js.ValueOf([]any{
			float64(d.Kind),
			d.Rate,
			d.SeekTo,
			boolToNum(d.Ready),
			boolToNum(d.Stalled),
			d.ActualRate,
			float64(d.PreloadPhase),
			boolToNum(d.ResetRate),
		})
	})
}

// videoSyncPlaybackStartFunc resets the warmup epoch for the engine under id.
//
// Call shape:
//
//	__gosx_video_sync_playback_start(id, perfNowMs)
func videoSyncPlaybackStartFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			return js.Null()
		}
		b.OnPlaybackStartVideoSync(args[0].String(), args[1].Float())
		return js.Null()
	})
}

// videoSyncDisposeFunc removes the engine registered under id.
//
// Call shape:
//
//	__gosx_video_sync_dispose(id)
func videoSyncDisposeFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.Null()
		}
		b.DisposeVideoSync(args[0].String())
		return js.Null()
	})
}

// videoSyncStatsFunc returns a JSON-encoded Stats snapshot. Cold path — not
// called per frame.
//
// Call shape:
//
//	__gosx_video_sync_stats(id, perfNowMs?)
//
// perfNowMs defaults to 0 when omitted.
func videoSyncStatsFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.Null()
		}
		id := args[0].String()
		var perfNowMs float64
		if len(args) >= 2 && args[1].Type() == js.TypeNumber {
			perfNowMs = args[1].Float()
		}
		s, err := b.StatsVideoSync(id, perfNowMs)
		if err != nil {
			return jsError(err)
		}
		return js.ValueOf(s)
	})
}

// videoSyncLastReasonFunc returns the last debug reason string from the engine
// under id. Cold/debug path.
//
// Call shape:
//
//	__gosx_video_sync_last_reason(id)
func videoSyncLastReasonFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.Null()
		}
		return js.ValueOf(b.LastReasonVideoSync(args[0].String()))
	})
}

// boolToNum converts a bool to a float64 suitable for the positional Tick
// response array. 1.0 = true, 0.0 = false.
func boolToNum(v bool) float64 {
	if v {
		return 1.0
	}
	return 0.0
}
