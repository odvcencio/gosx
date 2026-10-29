package bridge

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// viewBatches builds n state snapshots shaped like the view-model signals a UI
// engine publishes: one object per signal, with strings, small integers,
// booleans and short integer series for sparklines. Snapshot k differs from
// snapshot k-1 in every signal, so a changed-only apply finds real work.
func viewBatches(n, signals, rows, series int) []string {
	batches := make([]string, n)
	for k := range batches {
		batch := map[string]any{}
		for s := 0; s < signals; s++ {
			list := make([]map[string]any, rows)
			for r := range list {
				spark := make([]int, series)
				for i := range spark {
					spark[i] = (i*r + k + s) % 100
				}
				list[r] = map[string]any{
					"ID": fmt.Sprintf("row-%d", r), "Label": fmt.Sprintf("Row label number %d", r),
					"Detail": fmt.Sprintf("Detail text for row %d at tick %d", r, k), "Tone": r % 4,
					"Value": (r*7 + k) % 1000, "Spark": spark, "Key": fmt.Sprintf("select|row-%d", r),
					"Selected": r == k%rows, "Unit": "kW", "Trend": (k+r)%3 - 1,
				}
			}
			batch[fmt.Sprintf("$view.panel%d", s)] = map[string]any{"Title": "Systems", "Rows": list, "Footer": fmt.Sprintf("All systems nominal %d", k)}
		}
		data, err := json.Marshal(batch)
		if err != nil {
			panic(err)
		}
		batches[k] = string(data)
	}
	return batches
}

// BenchmarkSharedSignalBatch measures what the runtime does for one snapshot a
// worker or engine sends: decode it, find what changed, store it, and tell the
// browser about each change (the callback stands for the page). It reports the
// size of a batch, so run it natively and on js/wasm to compare builds.
func BenchmarkSharedSignalBatch(b *testing.B) {
	for _, shape := range []struct {
		name                 string
		signals, rows, spark int
	}{
		{"1signal-2KB", 1, 3, 12},
		{"3signals-8KB", 3, 6, 12},
		{"3signals-30KB", 3, 18, 24},
	} {
		batches := viewBatches(32, shape.signals, shape.rows, shape.spark)
		size := 0
		for _, text := range batches {
			size += len(text)
		}
		b.Run(shape.name, func(b *testing.B) {
			br := New()
			told := 0
			br.SetSharedSignalCallback(func(name, valueJSON string) { told += len(valueJSON) })
			// Take the first snapshot outside the timing so every timed apply replaces values.
			if err := br.SetChangedSharedSignalBatchJSON(batches[0]); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(size / len(batches)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := br.SetChangedSharedSignalBatchJSON(batches[(i+1)%len(batches)]); err != nil {
					b.Fatal(err)
				}
			}
			if told == 0 {
				b.Fatal("the callback was never told of a change")
			}
		})
	}
}

// TestViewBatchesChangeEverySignal keeps the benchmark honest: every timed
// snapshot must replace every signal, or the apply would skip its work.
func TestViewBatchesChangeEverySignal(t *testing.T) {
	batches := viewBatches(4, 3, 6, 12)
	br := New()
	var told []string
	br.SetSharedSignalCallback(func(name, _ string) { told = append(told, name) })
	for k, text := range batches {
		told = told[:0]
		if err := br.SetChangedSharedSignalBatchJSON(text); err != nil {
			t.Fatal(err)
		}
		if len(told) != 3 || strings.Count(strings.Join(told, ","), "$view.panel") != 3 {
			t.Fatalf("snapshot %d changed %v, want all three signals", k, told)
		}
	}
}
