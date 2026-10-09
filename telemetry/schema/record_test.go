package schema_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"m31labs.dev/gosx/internal/telemetryerr"
	"m31labs.dev/gosx/internal/telemetryrecord"
	"m31labs.dev/gosx/telemetry/schema"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func envelope() schema.Envelope {
	return schema.Envelope{Stream: "0123456789abcdef0123456789abcdef", Boot: "fedcba9876543210fedcba9876543210", Seq: 42, State: schema.StateFinal, At: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), Revision: 3}
}
func activity() schema.Activity {
	at := envelope().At
	return schema.Activity{ID: "10000000000000000000000000000000", Kind: "match", Dimensions: []schema.Dimension{{Name: "mode", Value: "team"}, {Name: "players", Value: "4"}}, StartedAt: at.Add(-time.Second), UpdatedAt: at, EndedAt: at, ElapsedMS: 1000, Outcome: "won", Reason: "complete", Codec: schema.Codec{Name: "match", Version: 1}, Fields: schema.Fields{{Name: "wave", Type: schema.FieldInt, Int: 3}, {Name: "mode", Type: schema.FieldEnum, Enum: "<&"}}}
}
func goldenRecords(t *testing.T) map[string]schema.Record {
	t.Helper()
	e := envelope()
	a, err := telemetryrecord.NewActivity(e, activity())
	if err != nil {
		t.Fatal(err)
	}
	v, err := telemetryrecord.NewVisit(e, schema.Visit{ID: "20000000000000000000000000000000", StartedAt: e.At.Add(-time.Second), UpdatedAt: e.At, EndedAt: e.At, ElapsedMS: 1000, ActiveMS: 500, EndReason: "pagehide", InitialRoute: "/", PageCount: 1, Client: schema.Client{Platform: "other", Browser: "other", Device: "unknown"}})
	if err != nil {
		t.Fatal(err)
	}
	h, err := telemetryrecord.NewHubSession(e, schema.HubSession{ID: "30000000000000000000000000000000", Hub: "room", StartedAt: e.At.Add(-time.Second), UpdatedAt: e.At, EndedAt: e.At, ElapsedMS: 1000, Reason: "normal", MessagesIn: 1, MessagesOut: 2, BytesIn: 3, BytesOut: 4})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]schema.Record{"activity": a, "visit": v, "hub_session": h}
}
func TestRecordGoldenEnvelopeAndChecksum(t *testing.T) {
	for name, r := range goldenRecords(t) {
		t.Run(name, func(t *testing.T) {
			expected, err := os.ReadFile(filepath.Join("testdata", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			got := r.Bytes()
			if !bytes.Equal(got, expected) {
				t.Fatalf("canonical record differs\ngot: %s\nwant: %s", got, expected)
			}
			if len(got) > 16<<10 || got[len(got)-1] != '\n' || bytes.Contains(got, []byte(`\u003c`)) {
				t.Fatal("size/newline/escaping contract")
			}
			var decoded struct {
				Schema int
				CRC32C string
			}
			if err := json.Unmarshal(got, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Schema != 1 || len(decoded.CRC32C) != 8 {
				t.Fatal(decoded)
			}
			marker := []byte(`,"crc32c":"` + decoded.CRC32C + `"`)
			input := bytes.Replace(bytes.TrimSuffix(got, []byte{'\n'}), marker, nil, 1)
			checksum := crc32.Checksum(input, crc32.MakeTable(crc32.Castagnoli))
			expectedCRC, err := strconv.ParseUint(decoded.CRC32C, 16, 32)
			if err != nil || uint64(checksum) != expectedCRC {
				t.Fatal("checksum input changed", err)
			}
		})
	}
}
func TestRecordsAndTypedViewsAreImmutable(t *testing.T) {
	input := activity()
	input.Fields = append(input.Fields, schema.Field{Name: "stats", Type: schema.FieldObject, Object: schema.Fields{{Name: "values", Type: schema.FieldFloats, Floats: []float64{1.5, 2.5}}}})
	input.Participants = []schema.Participant{{ID: "40000000000000000000000000000000", Seat: 0, Role: "player", Codec: schema.Codec{Name: "seat", Version: 1}, Sessions: []schema.SessionLink{{ID: "50000000000000000000000000000000"}}, Reasons: []schema.ReasonCount{{Reason: "normal", Count: 1}}, Fields: schema.Fields{{Name: "scores", Type: schema.FieldInts, Ints: []int64{2, 3}}}}}
	r, err := telemetryrecord.NewActivity(envelope(), input)
	if err != nil {
		t.Fatal(err)
	}
	before := r.Bytes()
	input.Dimensions[0].Value = "private-canary"
	input.Fields[0].Int = 99
	input.Fields[2].Object[0].Floats[0] = 99
	input.Participants[0].Fields[0].Ints[0] = 99
	view, ok := r.Activity()
	if !ok {
		t.Fatal("missing typed view")
	}
	view.Participants[0].Sessions[0].ID = "private-canary"
	view.Participants[0].Fields[0].Ints[0] = 99
	view.Fields[1].Enum = "private-canary"
	view.Fields[2].Object[0].Floats[0] = 99
	raw := r.Bytes()
	raw[0] = '!'
	e := r.Envelope()
	e.Stream = "private-canary"
	again, _ := r.Activity()
	if again.Participants[0].Fields[0].Ints[0] != 2 || again.Fields[0].Int != 3 || again.Fields[2].Object[0].Floats[0] != 1.5 || !bytes.Equal(before, r.Bytes()) {
		t.Fatal("returned or source data mutated the record")
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 50 {
				copy, _ := r.Activity()
				copy.Participants[0].Fields[0].Ints[0] = 7
				if !bytes.Equal(before, r.Bytes()) {
					t.Error("concurrent view changed record")
				}
			}
		})
	}
	wg.Wait()
}
func TestRecordValidationBoundsAndUnavailableHealth(t *testing.T) {
	for _, mutate := range []func(*schema.Activity){func(v *schema.Activity) { v.ElapsedMS = math.NaN() }, func(v *schema.Activity) { v.ID = "raw-user-canary" }, func(v *schema.Activity) { v.EndedAt = v.StartedAt.Add(-time.Second) }, func(v *schema.Activity) { v.Participants = make([]schema.Participant, 34) }, func(v *schema.Activity) {
		v.Fields = schema.Fields{{Name: "a", Type: schema.FieldInts, Ints: make([]int64, 33)}}
	}} {
		a := activity()
		mutate(&a)
		if _, err := telemetryrecord.NewActivity(envelope(), a); err == nil {
			t.Fatal("invalid payload admitted")
		}
	}
	a := activity()
	a.TickHealth = &schema.TickHealth{Available: false, MaxMS: 99}
	r, err := telemetryrecord.NewActivity(envelope(), a)
	if err != nil || strings.Contains(string(r.Bytes()), "tick_health") {
		t.Fatal("unknown health encoded as healthy data", err)
	}
	a.TickHealth = &schema.TickHealth{Available: true, Samples: 2, P50MS: 1, P99MS: 2, MaxMS: 1.95, BudgetMS: 20}
	r, err = telemetryrecord.NewActivity(envelope(), a)
	if err != nil || !strings.Contains(string(r.Bytes()), `"available":true`) {
		t.Fatal("available health omitted", err)
	}
	e := envelope()
	e.Schema = 2
	if _, err := telemetryrecord.NewActivity(e, a); !errors.Is(err, telemetryerr.ErrInvalidOptions) {
		t.Fatal("unknown schema accepted", err)
	}
	a = activity()
	a.Fields[0].Enum = strings.Repeat("unused", 100000)
	r, err = telemetryrecord.NewActivity(envelope(), a)
	if err != nil {
		t.Fatal(err)
	}
	view, _ := r.Activity()
	if view.Fields[0].Enum != "" {
		t.Fatal("inactive field member retained")
	}
}
func TestEventAndHealthTypedConstructors(t *testing.T) {
	e := envelope()
	e.State = schema.StateEvent
	e.Revision = 0
	r, err := telemetryrecord.NewActivityEvent(e, schema.ActivityEvent{ActivityID: activity().ID, Seq: 1, Name: "round", ObservedAt: e.At, Codec: schema.Codec{Name: "round", Version: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.ActivityEvent(); !ok {
		t.Fatal("missing event")
	}
	if _, ok := r.Activity(); ok {
		t.Fatal("wrong variant returned")
	}
	r, err = telemetryrecord.NewClientHealthSummary(e, schema.ClientHealthSummary{BucketAt: e.At, Route: "/", Code: "load_slow", RenderTier: "lite", Viewport: "small", Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.ClientHealthSummary(); !ok {
		t.Fatal("missing health")
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

type failedWriter struct{ err error }

func (w failedWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRecordWriteToDefensiveAndShortWriter(t *testing.T) {
	r := goldenRecords(t)["activity"]
	var b bytes.Buffer
	n, err := r.WriteTo(&b)
	if err != nil || n != int64(len(r.Bytes())) || !bytes.Equal(b.Bytes(), r.Bytes()) {
		t.Fatal(n, err)
	}
	if _, err := r.WriteTo(shortWriter{}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	canary := errors.New("private-writer-error-canary")
	if _, err := r.WriteTo(failedWriter{canary}); !errors.Is(err, canary) || strings.Contains(err.Error(), "canary") {
		t.Fatal("writer error lost its identity or exposed text", err)
	}
	if _, err := (schema.Record{}).WriteTo(&b); !errors.Is(err, telemetryerr.ErrInvalidOptions) {
		t.Fatal("zero record serialized", err)
	}
}
func BenchmarkCanonicalActivityRecord(b *testing.B) {
	v := activity()
	e := envelope()
	b.ReportAllocs()
	for range b.N {
		if _, err := telemetryrecord.NewActivity(e, v); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRecordAggregateAndCompleteByteCaps(t *testing.T) {
	a := activity()
	a.Fields = nil
	for i := 0; i < 32; i++ {
		a.Fields = append(a.Fields, schema.Field{Name: fmt.Sprintf("field_%02d", i), Type: schema.FieldEnum, Enum: strings.Repeat("x", 64)})
	}
	p := schema.Participant{ID: "40000000000000000000000000000000", Seat: 0, Role: "player", Codec: schema.Codec{Name: "seat", Version: 1}, Fields: a.Fields}
	a.Participants = []schema.Participant{p}
	if _, err := telemetryrecord.NewActivity(envelope(), a); !errors.Is(err, telemetryerr.ErrFieldBudget) {
		t.Fatal("aggregate field cap not enforced", err)
	}
	a = activity()
	a.Participants = nil
	for i := -1; i < 32; i++ {
		p := schema.Participant{ID: fmt.Sprintf("%032x", i+2), Seat: i, Role: "player", Codec: schema.Codec{Name: "seat", Version: 1}}
		for j := 0; j < 16; j++ {
			p.Sessions = append(p.Sessions, schema.SessionLink{ID: fmt.Sprintf("%032x", j+100)})
		}
		a.Participants = append(a.Participants, p)
	}
	if _, err := telemetryrecord.NewActivity(envelope(), a); !errors.Is(err, telemetryerr.ErrFieldBudget) {
		t.Fatal("complete record cap not enforced", err)
	}
}

func retainedActivity(n int) schema.Activity {
	a := activity()
	a.Fields, a.Dimensions = nil, nil
	for n > 0 {
		count := min(n, 64)
		fields := make(schema.Fields, count)
		for i := range fields {
			fields[i] = schema.Field{Name: fmt.Sprintf("f_%014d", i), Type: schema.FieldBool, Bool: true}
		}
		if a.Fields == nil {
			a.Fields = fields
		} else {
			i := len(a.Participants)
			a.Participants = append(a.Participants, schema.Participant{ID: fmt.Sprintf("%032x", i+10), Seat: i, Role: "player", Codec: schema.Codec{Name: "seat", Version: 1}, Fields: fields})
		}
		n -= count
	}
	return a
}

func TestRecordRejectsRetainedMemoryOverflow(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("retained-memory overflow shape requires 64-bit object layouts")
	}
	// These 147 bools fit the field and line limits, but the copied record
	// exceeds 32 KiB once allocation rounding is included.
	if _, err := telemetryrecord.NewActivity(envelope(), retainedActivity(147)); !errors.Is(err, telemetryerr.ErrFieldBudget) {
		t.Fatalf("retained-memory overflow: %v, want ErrFieldBudget", err)
	}
}

func TestRecordUTCAndShortestFiniteNumbers(t *testing.T) {
	e := envelope()
	e.At = e.At.In(time.FixedZone("private-zone-canary", 3600))
	a := activity()
	a.Fields = schema.Fields{{Name: "score", Type: schema.FieldFloat, Float: 1e21}}
	r, err := telemetryrecord.NewActivity(e, a)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(r.Bytes()); !strings.Contains(got, `"score":1e21`) || !strings.Contains(got, `"at":"2026-10-07T12:00:00Z"`) || strings.Contains(got, "canary") {
		t.Fatal("number/time canonicalization changed", got)
	}
	if r.Envelope().At.Location() != time.UTC {
		t.Fatal("envelope retained a local time zone")
	}
}
