//go:build !js || !wasm

package telemetry

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/telemetry/metric"
)

func activityDeclarationOwner(t *testing.T) *Telemetry {
	t.Helper()
	o := Defaults()
	o.Metrics.DisableRequests = true
	tel := &Telemetry{opts: o}
	if err := tel.initializeRegistry(); err != nil {
		t.Fatal(err)
	}
	tel.active.Store(true)
	return tel
}

func TestActivityKindDeclarationsCopiedAndFinite(t *testing.T) {
	tel := activityDeclarationOwner(t)
	opts := ActivityKindOptions{Dimensions: []Dimension{{Name: "mode", Values: []string{"team", "solo"}}, {Name: "players", Values: []string{"2", "4"}}}, Outcomes: []string{"won"}, Reasons: []string{"complete"}, Events: []string{"round"}, Roles: []string{"player"}}
	codecs := ActivityCodecs[int, NoFields, NoFields]{Activity: DomainCodec[int]{Name: "match", Version: 1, Fields: []FieldDefinition{{Name: "score", Type: FieldInt}}, Encode: func(f *FieldSet, v int) error { return f.Int("score", int64(v)) }}}
	kind, err := newActivityKind(tel, "match", opts, codecs)
	if err != nil {
		t.Fatal(err)
	}
	opts.Dimensions[0].Name = "mutated"
	opts.Dimensions[0].Values[0] = "mutated"
	opts.Outcomes[0] = "mutated"
	codecs.Activity.Fields[0].Name = "mutated"
	if kind.core.dimensions[0].Name != "mode" || kind.core.outcomes[0] != "won" {
		t.Fatal("retained mutable declaration")
	}
	for _, v := range [][2]string{{"team", "2"}, {"solo", "4"}} {
		if kind.core.dimensionTuple(v) != v {
			t.Fatal(v)
		}
	}
	if got := kind.core.dimensionTuple([2]string{"private-room", "42"}); got != [2]string{"other", "other"} {
		t.Fatal(got)
	}
	fields, err := kind.activity.encodeFields(tel.activities.pool, 12)
	if err != nil || len(fields) != 1 || fields[0].Name != "score" || fields[0].Int != 12 {
		t.Fatal(fields, err)
	}
	if _, err := newActivityKind(tel, "match", ActivityKindOptions{}, ActivityCodecs[NoFields, NoFields, NoFields]{}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	before := tel.registry.Usage()
	for i := 0; i < 1000; i++ {
		kind.core.dimensionTuple([2]string{fmt.Sprint(i), "private"})
	}
	if tel.registry.Usage() != before {
		t.Fatal("unknown dimensions admitted series")
	}
}

func TestActivityKindValidationRollbackAndSeal(t *testing.T) {
	cases := []ActivityKindOptions{
		{Dimensions: []Dimension{{Name: "mode", Values: []string{strings.Repeat("x", 65)}}}},
		{Dimensions: []Dimension{{Name: "mode", Values: nil}}},
		{Dimensions: []Dimension{{Name: "mode", Values: []string{"a"}}, {Name: "mode", Values: []string{"b"}}}},
		{Dimensions: []Dimension{{Name: "mode", Values: []string{"a", "a"}}}},
		{Dimensions: []Dimension{{Name: "mode", Values: []string{"a"}}}, Combinations: [][2]string{{"b", "none"}}},
		{Combinations: [][2]string{{"none", "none"}, {"none", "none"}}},
		{Outcomes: []string{"raw error"}},
		{Reasons: []string{"a", "a"}},
		{Events: make([]string, 65)},
		{Roles: make([]string, 9)},
	}
	for _, opts := range cases {
		tel := activityDeclarationOwner(t)
		before, bytes := tel.registry.Usage(), tel.miscBytes.Load()
		if _, err := newActivityKind(tel, "match", opts, ActivityCodecs[NoFields, NoFields, NoFields]{}); err == nil {
			t.Fatal("accepted invalid declaration", opts)
		}
		if tel.registry.Usage() != before || tel.miscBytes.Load() != bytes || len(tel.activities.kinds) != 0 {
			t.Fatal("partial invalid admission")
		}
	}
	tel := activityDeclarationOwner(t)
	before, bytes := tel.registry.Usage(), tel.miscBytes.Load()
	tel.authority.Seal()
	if _, err := newActivityKind(tel, "match", ActivityKindOptions{}, ActivityCodecs[NoFields, NoFields, NoFields]{}); !errors.Is(err, ErrAfterBuild) {
		t.Fatal(err)
	}
	before.Sealed = true
	if tel.registry.Usage() != before || tel.miscBytes.Load() != bytes {
		t.Fatal("sealed registration changed reservation")
	}
	tel.active.Store(false)
	if _, err := newActivityKind(tel, "match", ActivityKindOptions{}, ActivityCodecs[NoFields, NoFields, NoFields]{}); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestActivityKindsGlobalCapAndWholeMetricFailure(t *testing.T) {
	tel := activityDeclarationOwner(t)
	for i := 0; i < 32; i++ {
		if _, err := newActivityKind(tel, fmt.Sprintf("kind_%d", i), ActivityKindOptions{}, ActivityCodecs[NoFields, NoFields, NoFields]{}); err != nil {
			t.Fatal(i, err)
		}
	}
	before, bytes := tel.registry.Usage(), tel.miscBytes.Load()
	if _, err := newActivityKind(tel, "over", ActivityKindOptions{}, ActivityCodecs[NoFields, NoFields, NoFields]{}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if tel.registry.Usage() != before || tel.miscBytes.Load() != bytes {
		t.Fatal("cap changed reservations")
	}
	small := Defaults()
	small.Metrics.DisableRequests = true
	small.Metrics.MaxSeries = before.Samples - 1
	failed := &Telemetry{opts: small}
	if err := failed.initializeRegistry(); err != nil {
		t.Fatal(err)
	}
	failed.active.Store(true)
	for i := 0; i < 31; i++ {
		if _, err := newActivityKind(failed, fmt.Sprintf("kind_%d", i), ActivityKindOptions{}, ActivityCodecs[NoFields, NoFields, NoFields]{}); err != nil {
			t.Fatal(i, err)
		}
	}
	usage, reserved := failed.registry.Usage(), failed.miscBytes.Load()
	if _, err := newActivityKind(failed, "last", ActivityKindOptions{}, ActivityCodecs[NoFields, NoFields, NoFields]{}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if failed.registry.Usage() != usage || failed.miscBytes.Load() != reserved || len(failed.activities.kinds) != 31 {
		t.Fatal("failed batch leaked kind or bytes")
	}
}

func TestActivityDimensionShapesAndExplicitCombinations(t *testing.T) {
	tel := activityDeclarationOwner(t)
	values := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
	opts := ActivityKindOptions{Dimensions: []Dimension{{Name: "mode", Values: values}, {Name: "size", Values: values}}}
	if _, err := newActivityKind(tel, "wide", opts, ActivityCodecs[NoFields, NoFields, NoFields]{}); err == nil {
		t.Fatal("expanded more than 64 combinations")
	}
	opts.Combinations = [][2]string{{"a", "b"}}
	k, err := newActivityKind(tel, "wide", opts, ActivityCodecs[NoFields, NoFields, NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	if k.core.dimensionTuple([2]string{"a", "b"}) != [2]string{"a", "b"} || k.core.dimensionTuple([2]string{"a", "a"}) != [2]string{"other", "other"} {
		t.Fatal("ignored finite combination declaration")
	}
	if _, err = newActivityKind(tel, "plain", ActivityKindOptions{}, ActivityCodecs[NoFields, NoFields, NoFields]{}); err != nil {
		t.Fatal(err)
	}
	if err = tel.registry.WithSnapshot(context.Background(), func(s metric.Snapshot) error {
		for _, f := range s.Families {
			if f.Name != "gosx_activities_started_total" {
				continue
			}
			for _, row := range f.Series {
				var names []string
				for _, l := range row.Labels {
					names = append(names, l.Name)
				}
				if !reflect.DeepEqual(names, []string{"kind", "dim0", "dim1"}) {
					t.Fatal("varying semantic dimension label names", names)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestActivityKindDisabledValidation(t *testing.T) {
	for _, tel := range []*Telemetry{nil, {}, {opts: Defaults()}} {
		k, err := newActivityKind(tel, "match", ActivityKindOptions{}, ActivityCodecs[NoFields, NoFields, NoFields]{})
		if err != nil || k.core != nil {
			t.Fatal(k, err)
		}
		if _, err = newActivityKind(tel, "private/name", ActivityKindOptions{}, ActivityCodecs[NoFields, NoFields, NoFields]{}); err == nil || strings.Contains(err.Error(), "private/name") {
			t.Fatal(err)
		}
		if _, err = newActivityKind(tel, "match", ActivityKindOptions{}, ActivityCodecs[int, NoFields, NoFields]{}); err == nil {
			t.Fatal("disabled codec was not validated")
		}
	}
}

func TestSharedMiscReservationConcurrentAdmissionAndRelease(t *testing.T) {
	tel := activityDeclarationOwner(t)
	before := tel.miscBytes.Load()
	remaining := hubMiscBytes - before
	if !tel.reserveMisc(remaining) || tel.reserveMisc(1) {
		t.Fatal("shared cap")
	}
	usage := tel.registry.Usage()
	if _, err := newActivityKind(tel, "match", ActivityKindOptions{}, ActivityCodecs[NoFields, NoFields, NoFields]{}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if _, err := tel.NewHubGroup("room", HubOptions{}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if tel.registry.Usage() != usage {
		t.Fatal("memory rejection admitted samples")
	}
	tel.releaseMisc(remaining)
	group, err := tel.NewHubGroup("room", HubOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				if tel.reserveMisc(1024) {
					if tel.miscBytes.Load() > hubMiscBytes {
						t.Error("oversubscribed shared arena")
					}
					tel.releaseMisc(1024)
				}
			}
		}()
	}
	wg.Wait()
	baseline := tel.miscBytes.Load()
	h := hub.New("room")
	detach, err := group.Attach(h)
	if err != nil {
		t.Fatal(err)
	}
	detach()
	detach()
	if tel.miscBytes.Load() != baseline {
		t.Fatal("detach reservation was not exactly once")
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if int64(unsafe.Sizeof(fieldPool{})) > fieldPoolBytes {
		t.Fatal("staging arena exceeds charge")
	}
	runtime.KeepAlive(tel)
}

func TestActivityCodecMemoryRejectionBeforePublication(t *testing.T) {
	tel := activityDeclarationOwner(t)
	before, reserved := tel.registry.Usage(), tel.miscBytes.Load()
	values := make([]string, 128)
	for i := range values {
		values[i] = fmt.Sprintf("v%03d_%s", i, strings.Repeat("x", 59))
	}
	fields := make([]FieldDefinition, 64)
	for i := range fields {
		fields[i] = FieldDefinition{Name: fmt.Sprintf("f%d", i), Type: FieldEnum, Values: values}
	}
	codec := DomainCodec[int]{Name: "wide", Version: 1, Fields: fields, Encode: func(*FieldSet, int) error { return nil }}
	codecs := ActivityCodecs[int, int, int]{Activity: codec, Participant: codec, Event: codec}
	if _, err := newActivityKind(tel, "wide", ActivityKindOptions{}, codecs); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if tel.registry.Usage() != before || tel.miscBytes.Load() != reserved || len(tel.activities.kinds) != 0 {
		t.Fatal("large rejected codec retained state")
	}
}

func BenchmarkActivityDimensionTuple(b *testing.B) {
	core := &activityKindCore{dimensions: []Dimension{{Name: "mode"}, {Name: "players"}}, combinations: [][2]string{{"team", "4"}, {"solo", "1"}}}
	tuple := [2]string{"solo", "1"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if core.dimensionTuple(tuple) != tuple {
			b.Fatal("lost declaration")
		}
	}
}
