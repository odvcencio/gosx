package telemetry

import "testing"

func TestDisabledTelemetryWarmAllocs(t *testing.T) {
	var tel Telemetry
	if allocations := testing.AllocsPerRun(1000, func() { tel.Enabled(); tel.Metrics() }); allocations != 0 {
		t.Fatal(allocations)
	}
}

func BenchmarkTelemetryDisabledHandle(b *testing.B) {
	var tel Telemetry
	b.ReportAllocs()
	for b.Loop() {
		tel.Enabled()
		tel.Metrics()
	}
}
