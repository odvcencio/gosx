package perf

import (
	"strings"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/profiler"
	"github.com/chromedp/cdproto/runtime"
)

func TestSpreadOfCountsMissedFrames(t *testing.T) {
	s := SpreadOf([]float64{16, 16, 16, 17, 40})
	if s.N != 5 || s.P50 != 16 || s.Max != 40 || s.Over16 != 2 || s.Over33 != 1 || s.Late != 1 {
		t.Fatalf("spread = %+v", s)
	}
	if mean := (16*3 + 17 + 40) / 5.0; s.Mean < mean-1e-9 || s.Mean > mean+1e-9 {
		t.Fatalf("mean %v, want %v", s.Mean, mean)
	}
	if (SpreadOf(nil) != Spread{}) {
		t.Fatal("an empty series must give the zero spread")
	}
}

func TestHotFunctionsRanksSelfTimeWithCallers(t *testing.T) {
	node := func(id int64, name, script string, children ...int64) *profiler.ProfileNode {
		return &profiler.ProfileNode{ID: id, CallFrame: &runtime.CallFrame{FunctionName: name, ScriptID: cdp.ScriptID(script)}, Children: children}
	}
	p := &profiler.Profile{
		Nodes:      []*profiler.ProfileNode{node(1, "(root)", "0", 2), node(2, "frame", "7", 3), node(3, "draw", "7")},
		Samples:    []int64{3, 3, 2},
		TimeDeltas: []int64{1000, 1000, 1000},
	}
	out := HotFunctions(p, 2, func(string) string { return "" })
	if len(out) != 3 || !strings.Contains(out[0], "script 7 in all") || !strings.Contains(out[1], "draw") || !strings.Contains(out[1], "< frame") {
		t.Fatalf("hot list:\n%s", strings.Join(out, "\n"))
	}
}
