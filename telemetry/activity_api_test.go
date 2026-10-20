//go:build !js || !wasm

package telemetry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/telemetry"
	"m31labs.dev/gosx/telemetry/schema"
)

type matchFields struct{ Wave int64 }
type seatFields struct{ Score int64 }
type roundFields struct{ Round int64 }

func TestTypedActivityAPIUsesMemoryWorker(t *testing.T) {
	t.Setenv("GOSX_TELEMETRY", "on")
	opts := telemetry.Defaults()
	opts.Listen.Addr = "off"
	opts.Metrics.DisableRequests = true
	opts.Metrics.DisableOperations = true
	opts.Metrics.DisableClientEvents = true
	opts.Metrics.DisableRuntime = true
	opts.Metrics.DisableReadiness = true
	opts.Metrics.DisableScheduled = true
	app := server.New()
	tel, err := telemetry.Enable(app, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tel.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	codecs := telemetry.ActivityCodecs[matchFields, seatFields, roundFields]{
		Activity:    telemetry.DomainCodec[matchFields]{Name: "match", Version: 1, Fields: []telemetry.FieldDefinition{{Name: "wave", Type: telemetry.FieldInt}}, Encode: func(f *telemetry.FieldSet, v matchFields) error { return f.Int("wave", v.Wave) }},
		Participant: telemetry.DomainCodec[seatFields]{Name: "seat", Version: 1, Fields: []telemetry.FieldDefinition{{Name: "score", Type: telemetry.FieldInt}}, Encode: func(f *telemetry.FieldSet, v seatFields) error { return f.Int("score", v.Score) }},
		Event:       telemetry.DomainCodec[roundFields]{Name: "round", Version: 1, Fields: []telemetry.FieldDefinition{{Name: "round", Type: telemetry.FieldInt}}, Encode: func(f *telemetry.FieldSet, v roundFields) error { return f.Int("round", v.Round) }},
	}
	kind, err := telemetry.NewActivityKind(tel, "match", telemetry.ActivityKindOptions{Events: []string{"round"}, Outcomes: []string{"won"}}, codecs)
	if err != nil {
		t.Fatal(err)
	}
	app.Build()
	if _, err = telemetry.NewActivityKind(tel, "late", telemetry.ActivityKindOptions{}, codecs); !errors.Is(err, telemetry.ErrAfterBuild) {
		t.Fatal(err)
	}
	match, err := kind.Begin(telemetry.ActivityStart[matchFields]{Fields: matchFields{Wave: 1}})
	if err != nil {
		t.Fatal(err)
	}
	seat, err := match.Participant(telemetry.ParticipantStart[seatFields]{Seat: 0, Human: false, Fields: seatFields{Score: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if err = seat.Set(seatFields{Score: 4}); err != nil {
		t.Fatal(err)
	}
	if err = match.Set(matchFields{Wave: 2}); err != nil {
		t.Fatal(err)
	}
	if err = match.Event("round", roundFields{Round: 1}); err != nil {
		t.Fatal(err)
	}
	receipt, err := match.End(telemetry.ActivityEnd[matchFields]{Outcome: "won", Fields: matchFields{Wave: 2}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = receipt.Wait(ctx); err != nil || receipt.Persistence() != schema.PersistenceMemory {
		t.Fatal(err, receipt.Persistence())
	}
	view, err := match.Snapshot()
	if err != nil || view.EventsAccepted != 1 || view.Participants[0].Fields[0].Int != 4 || view.Fields[0].Int != 2 {
		t.Fatal(view, err)
	}
	duplicate, err := match.End(telemetry.ActivityEnd[matchFields]{Outcome: "won", Fields: matchFields{Wave: 2}})
	if err != nil || duplicate != receipt {
		t.Fatal("terminal operation changed", err)
	}
	if _, err = match.End(telemetry.ActivityEnd[matchFields]{Outcome: "lost", Fields: matchFields{Wave: 2}}); !errors.Is(err, telemetry.ErrConflict) {
		t.Fatal(err)
	}
}

func TestDisabledActivityAPIKeepsHandlesInert(t *testing.T) {
	kind, err := telemetry.NewActivityKind[telemetry.NoFields, telemetry.NoFields, telemetry.NoFields](nil, "match", telemetry.ActivityKindOptions{}, telemetry.ActivityCodecs[telemetry.NoFields, telemetry.NoFields, telemetry.NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	activity, err := kind.Begin(telemetry.ActivityStart[telemetry.NoFields]{})
	if err != nil || activity.ID() != "" {
		t.Fatal(err)
	}
	participant, err := activity.Participant(telemetry.ParticipantStart[telemetry.NoFields]{})
	if err != nil || participant.ID() != "" {
		t.Fatal(err)
	}
	if err = activity.Event("round", telemetry.NoFields{}); err != nil {
		t.Fatal(err)
	}
	receipt, err := activity.End(telemetry.ActivityEnd[telemetry.NoFields]{})
	if err != nil || receipt.Persistence() != schema.PersistenceNone {
		t.Fatal(err)
	}
	if err = receipt.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
