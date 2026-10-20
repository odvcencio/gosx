//go:build !js && !wasm

package schema_test

import (
	"errors"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/telemetryerr"
	"m31labs.dev/gosx/internal/telemetryrecord"
	"m31labs.dev/gosx/telemetry/schema"
)

func TestRecordRetainedMemory(t *testing.T) {
	previous := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previous)

	measure := func(t *testing.T, input schema.Activity, allowRejection bool) {
		t.Helper()
		// Warm reflection/checksum caches before measuring. A rejection is
		// also safe: the oversized shape retains no record.
		r, err := telemetryrecord.NewActivity(envelope(), input)
		if allowRejection && errors.Is(err, telemetryerr.ErrFieldBudget) {
			t.Log("shape rejected by record bounds")
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		charge := telemetryrecord.RetainedBytes(r)
		const count = 512
		records := make([]schema.Record, count)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		for i := range records {
			records[i], err = telemetryrecord.NewActivity(envelope(), input)
			if err != nil {
				t.Fatal(err)
			}
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(input)
		runtime.KeepAlive(records)
		// The backing array existed at baseline; charge its inline metadata
		// separately. All other bytes come from the live heap delta, without
		// reusing the constructor's retained-accounting formula.
		measured := (int64(after.HeapAlloc)-int64(before.HeapAlloc))/count + int64(reflect.TypeFor[schema.Record]().Size())
		t.Logf("charge=%d measured=%d limit=%d", charge, measured, 32<<10)
		if measured > 32<<10 {
			t.Errorf("accepted record exceeds retained limit: %d > %d", measured, 32<<10)
		}
		if charge < measured {
			t.Errorf("retained charge understates live storage: %d < %d", charge, measured)
		}
	}

	t.Run("reported_shape", func(t *testing.T) {
		measure(t, retainedActivity(147), true)
	})
	t.Run("largest_accepted", func(t *testing.T) {
		largest := 0
		for n := 1; n <= 147; n++ {
			_, err := telemetryrecord.NewActivity(envelope(), retainedActivity(n))
			if err == nil {
				largest = n
			} else if !errors.Is(err, telemetryerr.ErrFieldBudget) {
				t.Fatal(err)
			}
		}
		if largest == 0 {
			t.Fatal("no bool-field activity accepted")
		}
		t.Logf("fields=%d", largest)
		measure(t, retainedActivity(largest), false)
	})
	t.Run("nested_storage", func(t *testing.T) {
		a := activity()
		a.Identity = &schema.Identity{App: strings.Repeat("a", 128), Version: strings.Repeat("v", 128), Revision: strings.Repeat("r", 128)}
		a.TickHealth = &schema.TickHealth{Available: true, Samples: 1, BudgetMS: 1}
		enums := make([]string, 17)
		for i := range enums {
			enums[i] = strings.Repeat("e", 17)
		}
		a.Fields = schema.Fields{{Name: "stats", Type: schema.FieldObject, Object: schema.Fields{
			{Name: "ints", Type: schema.FieldInts, Ints: make([]int64, 17)},
			{Name: "floats", Type: schema.FieldFloats, Floats: make([]float64, 17)},
			{Name: "enums", Type: schema.FieldEnums, Enums: enums},
		}}}
		a.Participants = []schema.Participant{{
			ID: "40000000000000000000000000000000", Seat: 0, Role: "player", Codec: schema.Codec{Name: "seat", Version: 1},
			Client:   &schema.Client{Platform: "other", Browser: "other", Device: "unknown"},
			Reasons:  []schema.ReasonCount{{Reason: "normal", Count: 1}},
			Sessions: []schema.SessionLink{{ID: "50000000000000000000000000000000"}},
		}}
		measure(t, a, false)
	})
}
