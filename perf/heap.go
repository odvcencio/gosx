package perf

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/heapprofiler"
	"github.com/chromedp/chromedp"
)

// TakeHeapSnapshot captures a Chrome heap snapshot and returns it as bytes
// ready to write to a .heapsnapshot file. Load the file into Chrome
// DevTools' Memory panel (Load…) to inspect retainers, compare snapshots,
// and find leaks.
//
// HeapProfiler streams the snapshot as sequential EventAddHeapSnapshotChunk
// events containing JSON string fragments — we listen, concatenate, and
// return the assembled document.
//
// Running garbage collection first is recommended — otherwise the snapshot
// includes ephemeral allocations from recent JS execution that aren't
// actually leaks. Call TakeHeapSnapshotAfterGC to get the cleaner variant.
func TakeHeapSnapshot(d *Driver) ([]byte, error) {
	return takeHeapSnapshot(d, false)
}

// TakeHeapSnapshotAfterGC runs CollectGarbage before capturing, giving
// cleaner numbers for leak analysis.
func TakeHeapSnapshotAfterGC(d *Driver) ([]byte, error) {
	return takeHeapSnapshot(d, true)
}

func takeHeapSnapshot(d *Driver, collectGC bool) ([]byte, error) {
	if d == nil {
		return nil, fmt.Errorf("perf: nil driver")
	}

	// Collect chunks via a listener on the driver context. The listener
	// goroutine appends under a mutex, so a concurrent Entries-style read
	// would be safe — but here we only read after tracing completes.
	var (
		mu     sync.Mutex
		chunks []string
	)

	listenCtx, cancelListen := context.WithCancel(d.ctx)
	defer cancelListen()

	chromedp.ListenTarget(listenCtx, func(ev interface{}) {
		if e, ok := ev.(*heapprofiler.EventAddHeapSnapshotChunk); ok {
			mu.Lock()
			chunks = append(chunks, e.Chunk)
			mu.Unlock()
		}
	})

	// CDP commands need an executor-scoped context bound to the active
	// target; see coverage.go for the same pattern.
	execCtx := cdp.WithExecutor(d.ctx, chromedp.FromContext(d.ctx).Target)

	// HeapProfiler has to be enabled before snapshots can be taken.
	if err := heapprofiler.Enable().Do(execCtx); err != nil {
		return nil, fmt.Errorf("heapprofiler.Enable: %w", err)
	}
	defer func() { _ = heapprofiler.Disable().Do(execCtx) }()

	if collectGC {
		if err := heapprofiler.CollectGarbage().Do(execCtx); err != nil {
			return nil, fmt.Errorf("heapprofiler.CollectGarbage: %w", err)
		}
	}

	// Take the snapshot. The Do call returns when the last chunk event
	// fires — no separate "complete" signal needed.
	if err := heapprofiler.TakeHeapSnapshot().Do(execCtx); err != nil {
		return nil, fmt.Errorf("heapprofiler.TakeHeapSnapshot: %w", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(chunks) == 0 {
		return nil, fmt.Errorf("heap snapshot: no chunks received")
	}
	return []byte(strings.Join(chunks, "")), nil
}

// MemoryStats is a lightweight memory summary using Performance.memory +
// DOM counters — suitable for checking delta between a baseline and an
// after-interaction state. For deep retainer analysis, use the full
// heap snapshot.
type MemoryStats struct {
	JSHeapUsedMB  float64 `json:"jsHeapUsedMb"`
	JSHeapTotalMB float64 `json:"jsHeapTotalMb"`
	JSHeapLimitMB float64 `json:"jsHeapLimitMb"`
	DOMNodeCount  int     `json:"domNodeCount"`
	ListenerCount int     `json:"listenerCount"`
}

// QueryMemoryStats reads performance.memory + document DOM counts via a
// single JS eval. Returns zeros on non-Chrome contexts where the APIs
// aren't exposed.
func QueryMemoryStats(d *Driver) (MemoryStats, error) {
	var raw struct {
		Used   float64 `json:"used"`
		Total  float64 `json:"total"`
		Limit  float64 `json:"limit"`
		Nodes  int     `json:"nodes"`
		Listen int     `json:"listen"`
	}
	err := d.Evaluate(`(function(){
		var m = performance.memory || {};
		var nodes = document.getElementsByTagName('*').length;
		return {
			used:   m.usedJSHeapSize   ? m.usedJSHeapSize/(1024*1024)   : 0,
			total:  m.totalJSHeapSize  ? m.totalJSHeapSize/(1024*1024)  : 0,
			limit:  m.jsHeapSizeLimit  ? m.jsHeapSizeLimit/(1024*1024)  : 0,
			nodes:  nodes,
			listen: 0
		};
	})()`, &raw)
	if err != nil {
		return MemoryStats{}, err
	}
	return MemoryStats{
		JSHeapUsedMB:  raw.Used,
		JSHeapTotalMB: raw.Total,
		JSHeapLimitMB: raw.Limit,
		DOMNodeCount:  raw.Nodes,
		ListenerCount: raw.Listen,
	}, nil
}
