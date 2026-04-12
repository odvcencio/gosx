package perf

// PerfEntry represents a performance.measure entry.
type PerfEntry struct {
	Name      string  `json:"name"`
	StartTime float64 `json:"startTime"`
	Duration  float64 `json:"duration"`
}

// NavigationTiming holds page lifecycle timestamps.
type NavigationTiming struct {
	TTFB             float64 // responseStart - requestStart
	DOMContentLoaded float64 // domContentLoadedEventEnd - navigationStart
	FullyLoaded      float64 // loadEventEnd - navigationStart
}

// RuntimeState holds GoSX runtime introspection.
type RuntimeState struct {
	IslandCount     int  `json:"islandCount"`
	EngineCount     int  `json:"engineCount"`
	PerfReady       bool `json:"perfReady"`
	FirstFrame      bool `json:"firstFrame"`
	FrameCount      int  `json:"frameCount"`
	HubMessageCount int  `json:"hubMessageCount"`
}

// HydrationEntry from __gosx_perf.hydrationLog.
type HydrationEntry struct {
	ID string  `json:"id"`
	Ms float64 `json:"ms"`
}

// DispatchEntry from __gosx_perf.dispatchLog.
type DispatchEntry struct {
	Island  string  `json:"island"`
	Handler string  `json:"handler"`
	Ms      float64 `json:"ms"`
	Patches int     `json:"patches"`
}

// QueryPerformanceMeasures returns all performance.measure entries whose name
// starts with prefix.
func QueryPerformanceMeasures(d *Driver, prefix string) ([]PerfEntry, error) {
	var entries []PerfEntry
	err := d.Evaluate(`
		performance.getEntriesByType("measure")
			.filter(e => e.name.startsWith(`+jsString(prefix)+`))
			.map(e => ({name: e.name, startTime: e.startTime, duration: e.duration}))
	`, &entries)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []PerfEntry{}
	}
	return entries, nil
}

// QueryNavigationTiming reads the first navigation timing entry and computes
// TTFB, DOMContentLoaded, and FullyLoaded durations.
func QueryNavigationTiming(d *Driver) (NavigationTiming, error) {
	var raw struct {
		TTFB float64 `json:"ttfb"`
		DCL  float64 `json:"dcl"`
		Full float64 `json:"full"`
	}
	err := d.Evaluate(`(function(){
		var n = performance.getEntriesByType("navigation")[0];
		if (!n) return {ttfb:0, dcl:0, full:0};
		return {
			ttfb: n.responseStart - n.requestStart,
			dcl:  n.domContentLoadedEventEnd - n.startTime,
			full: n.loadEventEnd - n.startTime
		};
	})()`, &raw)
	if err != nil {
		return NavigationTiming{}, err
	}
	return NavigationTiming{
		TTFB:             raw.TTFB,
		DOMContentLoaded: raw.DCL,
		FullyLoaded:      raw.Full,
	}, nil
}

// QueryHeapSize returns the used JS heap size in MB. If the API is not
// available (non-Chrome or restricted context), it returns 0 with no error.
func QueryHeapSize(d *Driver) (float64, error) {
	var mb float64
	err := d.Evaluate(`(function(){
		if (performance.memory && performance.memory.usedJSHeapSize) {
			return performance.memory.usedJSHeapSize / (1024*1024);
		}
		return 0;
	})()`, &mb)
	if err != nil {
		return 0, err
	}
	return mb, nil
}

// QueryRuntimeState reads GoSX runtime globals and the perf namespace.
func QueryRuntimeState(d *Driver) (RuntimeState, error) {
	var st RuntimeState
	err := d.Evaluate(`(function(){
		var r = {
			islandCount: 0,
			engineCount: 0,
			perfReady: false,
			firstFrame: false,
			frameCount: 0,
			hubMessageCount: 0
		};
		if (window.__gosx) {
			if (window.__gosx.islands) r.islandCount = window.__gosx.islands.size || 0;
			if (window.__gosx.engines) r.engineCount = window.__gosx.engines.size || 0;
		}
		var p = window.__gosx_perf;
		if (p) {
			r.perfReady = !!p.ready;
			r.firstFrame = !!p.firstFrame;
			r.frameCount = p.frameCount || 0;
			r.hubMessageCount = p.hubMessageCount || 0;
		}
		return r;
	})()`, &st)
	return st, err
}

// QueryHydrationLog returns the hydration log from __gosx_perf.
func QueryHydrationLog(d *Driver) ([]HydrationEntry, error) {
	var entries []HydrationEntry
	err := d.Evaluate(`(window.__gosx_perf && window.__gosx_perf.hydrationLog) || []`, &entries)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []HydrationEntry{}
	}
	return entries, nil
}

// QueryDispatchLog returns the dispatch log from __gosx_perf.
func QueryDispatchLog(d *Driver) ([]DispatchEntry, error) {
	var entries []DispatchEntry
	err := d.Evaluate(`(window.__gosx_perf && window.__gosx_perf.dispatchLog) || []`, &entries)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []DispatchEntry{}
	}
	return entries, nil
}

// QuerySceneFrames is a shorthand for QueryPerformanceMeasures with the
// "scene3d-render" prefix.
func QuerySceneFrames(d *Driver) ([]PerfEntry, error) {
	return QueryPerformanceMeasures(d, "scene3d-render")
}

// jsString returns a JavaScript string literal for s, using JSON encoding
// to handle escaping.
func jsString(s string) string {
	// JSON-encode produces a valid JS string literal with proper escaping.
	b := []byte{'"'}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '\t':
			b = append(b, '\\', 't')
		default:
			if c < 0x20 {
				b = append(b, '\\', 'u', '0', '0', hexDigit(c>>4), hexDigit(c&0xf))
			} else {
				b = append(b, c)
			}
		}
	}
	b = append(b, '"')
	return string(b)
}

func hexDigit(b byte) byte {
	if b < 10 {
		return '0' + b
	}
	return 'a' + b - 10
}
