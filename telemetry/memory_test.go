//go:build !js || !wasm

package telemetry

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/telemetry/metric"
)

// Exercise the production descriptor/admission functions with real Defaults,
// rather than synthesizing unrelated stand-ins for default aggregate families.
func defaultInventory(t *testing.T, opts Options) *Telemetry {
	t.Helper()
	tel := &Telemetry{opts: opts, start: Instant{Wall: time.Unix(100, 0)}}
	if err := tel.initializeRegistry(); err != nil {
		t.Fatal(err)
	}
	tel.active.Store(true)
	if _, err := tel.NewHubGroup("room", HubOptions{Events: []string{"move"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := tel.NewLoopKind("simulation", LoopOptions{Budget: 20 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewActivityKind(tel, "match", ActivityKindOptions{
		Dimensions: []Dimension{{Name: "mode", Values: []string{"team"}}, {Name: "players", Values: []string{"4"}}},
		Outcomes:   []string{"won", "lost"},
	}, ActivityCodecs[NoFields, NoFields, NoFields]{}); err != nil {
		t.Fatal(err)
	}
	return tel
}

func inventoryRoutes(methods ...string) []server.ObservationPattern {
	rows := make([]server.ObservationPattern, 512)
	for i := range rows {
		rows[i] = server.ObservationPattern{Kind: "page", Pattern: fmt.Sprintf("/route/%03d", i), Methods: methods}
	}
	return rows
}

func TestRealDefaultMetricInventoryFits(t *testing.T) {
	for _, methods := range [][]string{{"GET", "HEAD"}, {"*"}} {
		t.Run(methods[0], func(t *testing.T) {
			opts := Defaults()
			rows := inventoryRoutes(methods...)
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			tel := defaultInventory(t, opts)
			beforeRoutes := tel.registry.Usage()
			tel.observeCatalog(rows)
			usage := tel.registry.Usage()
			if !usage.Sealed || usage.Samples > opts.Metrics.MaxSeries || usage.Bytes > 8<<20 {
				t.Fatal("default inventory exceeded unchanged caps", usage)
			}
			admitted := (len(tel.requests.table.Load().rows) - 6) / 2
			if admitted == 0 || admitted > opts.Metrics.MaxRoutePatterns {
				t.Fatal("invalid whole-route admission", admitted)
			}
			if err := tel.registry.WithSnapshot(context.Background(), func(snapshot metric.Snapshot) error {
				found := make(map[string]bool)
				for _, family := range snapshot.Families {
					for _, series := range family.Series {
						for _, label := range series.Labels {
							if label.Value == "room" || label.Value == "simulation" || label.Value == "match" {
								found[label.Value] = true
							}
						}
						if family.Name == "gosx_http_request_duration_seconds" {
							for _, label := range series.Labels {
								if label.Name == "kind" && label.Value == "error" {
									t.Fatal("derived error reserved its own duration histogram")
								}
							}
						}
					}
				}
				for _, kind := range []string{"room", "simulation", "match"} {
					if !found[kind] {
						t.Fatal("route admission displaced a subsystem", kind)
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			runtime.ReadMemStats(&after)
			retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
			charged := usage.Bytes + tel.ownerBytes + tel.adapterBytes.Load() + tel.hubs.bytes.Load() + tel.loops.bytes.Load()
			charged += tel.activities.bytes.Load()
			if charged > opts.Limits.MemoryBudgetBytes {
				t.Fatalf("default reservations %d exceed total memory budget %d", charged, opts.Limits.MemoryBudgetBytes)
			}
			if retained > charged {
				t.Fatalf("default retained heap %d exceeds reservation %d", retained, charged)
			}
			t.Logf("methods=%v reserved_before_routes=%d admitted_routes=%d samples=%d sample_headroom=%d registry_bytes=%d byte_headroom=%d charged=%d retained=%d", methods, beforeRoutes.Samples, admitted, usage.Samples, opts.Metrics.MaxSeries-usage.Samples, usage.Bytes, (8<<20)-usage.Bytes, charged, retained)
			runtime.KeepAlive(tel)
			runtime.KeepAlive(rows)
		})
	}
}
