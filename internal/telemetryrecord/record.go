package telemetryrecord

import (
	"hash/crc32"
	"io"
	"m31labs.dev/gosx/internal/telemetryerr"
	"m31labs.dev/gosx/internal/telemetryfields"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Record is immutable. Only typed framework constructors can initialize it.
// Sinks can read copied views; there is no raw body mutation/submission API.
type Record struct {
	envelope Envelope
	line     string
	visit    *Visit
	hub      *HubSession
	activity *Activity
	event    *ActivityEvent
	health   *ClientHealthSummary
	charge   int64
}

func (r Record) Envelope() Envelope { return r.envelope }
func (r Record) Bytes() []byte      { return []byte(r.line) }
func (r Record) WriteTo(w io.Writer) (int64, error) {
	if w == nil || r.line == "" {
		return 0, telemetryerr.ErrInvalidOptions
	}
	n, err := io.WriteString(w, r.line)
	if err == nil && n != len(r.line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		err = &writeError{cause: err}
	}
	return int64(n), err
}

type writeError struct{ cause error }

func (e *writeError) Error() string { return "telemetry: record write failed" }
func (e *writeError) Unwrap() error { return e.cause }
func (r Record) Visit() (Visit, bool) {
	if r.visit == nil {
		return Visit{}, false
	}
	return clone(*r.visit), true
}
func (r Record) HubSession() (HubSession, bool) {
	if r.hub == nil {
		return HubSession{}, false
	}
	return clone(*r.hub), true
}
func (r Record) Activity() (Activity, bool) {
	if r.activity == nil {
		return Activity{}, false
	}
	return clone(*r.activity), true
}
func (r Record) ActivityEvent() (ActivityEvent, bool) {
	if r.event == nil {
		return ActivityEvent{}, false
	}
	return clone(*r.event), true
}
func (r Record) ClientHealthSummary() (ClientHealthSummary, bool) {
	if r.health == nil {
		return ClientHealthSummary{}, false
	}
	return *r.health, true
}

// RetainedBytes includes copied payloads, encoded bytes and record metadata.
// It is internal accounting, not an additional public schema API.
func RetainedBytes(r Record) int64 { return r.charge }
func Valid(r Record) bool          { return r.line != "" }

func NewVisit(e Envelope, v Visit) (Record, error) {
	if !entityState(e.State, v.EndedAt) {
		return Record{}, telemetryerr.ErrInvalidOptions
	}
	if err := validateVisit(&v); err != nil {
		return Record{}, err
	}
	v = clone(v)
	r, err := newRecord(e, TypeVisit, v)
	if err != nil {
		return Record{}, err
	}
	r.visit = &v
	return r, nil
}
func NewHubSession(e Envelope, v HubSession) (Record, error) {
	if !entityState(e.State, v.EndedAt) {
		return Record{}, telemetryerr.ErrInvalidOptions
	}
	if err := validateHub(&v); err != nil {
		return Record{}, err
	}
	v = clone(v)
	r, err := newRecord(e, TypeHubSession, v)
	if err != nil {
		return Record{}, err
	}
	r.hub = &v
	return r, nil
}
func NewActivity(e Envelope, v Activity) (Record, error) {
	if !entityState(e.State, v.EndedAt) {
		return Record{}, telemetryerr.ErrInvalidOptions
	}
	if err := validateActivity(&v); err != nil {
		return Record{}, err
	}
	v = clone(v)
	r, err := newRecord(e, TypeActivity, v)
	if err != nil {
		return Record{}, err
	}
	r.activity = &v
	return r, nil
}
func NewActivityEvent(e Envelope, v ActivityEvent) (Record, error) {
	if err := validateEvent(&v); err != nil {
		return Record{}, err
	}
	v = clone(v)
	r, err := newRecord(e, TypeActivityEvent, v)
	if err != nil {
		return Record{}, err
	}
	r.event = &v
	return r, nil
}
func NewClientHealthSummary(e Envelope, v ClientHealthSummary) (Record, error) {
	if err := validateHealth(&v); err != nil {
		return Record{}, err
	}
	v = clone(v)
	r, err := newRecord(e, TypeClientHealthSummary, v)
	if err != nil {
		return Record{}, err
	}
	r.health = &v
	return r, nil
}

func newRecord(e Envelope, typ RecordType, payload any) (Record, error) {
	if (e.Schema != 0 && e.Schema != SchemaVersion) || !id(e.Stream) || !id(e.Boot) || e.Seq == 0 || !timestamp(e.At) || (e.Type != "" && e.Type != typ) || e.CRC32C != "" {
		return Record{}, telemetryerr.ErrInvalidOptions
	}
	if typ == TypeActivityEvent || typ == TypeClientHealthSummary {
		if e.State != StateEvent {
			return Record{}, telemetryerr.ErrInvalidOptions
		}
	} else if (e.State != StateOpen && e.State != StateCheckpoint && e.State != StateFinal) || e.Revision == 0 {
		return Record{}, telemetryerr.ErrInvalidOptions
	}
	e.Schema = SchemaVersion
	e.Type = typ
	e.At = e.At.UTC().Round(0)
	e.Stream = strings.Clone(e.Stream)
	e.Boot = strings.Clone(e.Boot)
	// The checksum excludes only crc32c and the final newline.
	input, err := canonicalEnvelope(payload, false, e)
	if err != nil {
		return Record{}, err
	}
	sum := strconv.FormatUint(uint64(crc32.Checksum(input, crc32.MakeTable(crc32.Castagnoli))), 16)
	e.CRC32C = strings.Repeat("0", 8-len(sum)) + sum
	line, err := canonicalEnvelope(payload, true, e)
	if err != nil {
		return Record{}, err
	}
	if len(line)+1 > MaxRecordBytes {
		return Record{}, telemetryerr.ErrFieldBudget
	}
	line = append(line, '\n')
	charge := int64(reflect.TypeFor[Record]().Size()) + int64(len(line)) + 80 + retained(reflect.ValueOf(payload))
	if charge > 32<<10 {
		return Record{}, telemetryerr.ErrFieldBudget
	}
	return Record{envelope: e, line: string(line), charge: charge}, nil
}

// Encode envelope explicitly so the payload never travels through a public
// interface/raw body, and both checksum and output share one byte writer.
func canonicalEnvelope(payload any, withCRC bool, e Envelope) ([]byte, error) {
	w := jsonWriter{buf: make([]byte, 0, MaxRecordBytes)}
	w.add('{')
	fields := []struct {
		name  string
		value any
	}{{"schema", e.Schema}, {"stream", e.Stream}, {"boot", e.Boot}, {"seq", e.Seq}, {"type", e.Type}, {"state", e.State}, {"at", e.At}, {"revision", e.Revision}}
	for i, f := range fields {
		if i > 0 {
			w.add(',')
		}
		w.quoted(f.name)
		w.add(':')
		w.value(reflect.ValueOf(f.value))
	}
	if withCRC {
		w.text(`,"crc32c":`)
		w.quoted(e.CRC32C)
	}
	w.text(`,"rec":`)
	w.value(reflect.ValueOf(payload))
	w.add('}')
	return w.buf, w.err
}

func clone[T any](v T) T { return cloneValue(reflect.ValueOf(v)).Interface().(T) }
func cloneValue(v reflect.Value) reflect.Value {
	if v.Type() == reflect.TypeFor[time.Time]() {
		return reflect.ValueOf(v.Interface().(time.Time).UTC().Round(0))
	}
	if v.Type() == reflect.TypeFor[telemetryfields.Fields]() {
		input := v.Interface().(telemetryfields.Fields)
		out := make(telemetryfields.Fields, len(input))
		for i, f := range input {
			g := telemetryfields.Field{Name: strings.Clone(f.Name), Type: f.Type}
			switch f.Type {
			case telemetryfields.FieldInt:
				g.Int = f.Int
			case telemetryfields.FieldFloat:
				g.Float = f.Float
			case telemetryfields.FieldBool:
				g.Bool = f.Bool
			case telemetryfields.FieldDuration:
				g.Duration = f.Duration
			case telemetryfields.FieldEnum:
				g.Enum = strings.Clone(f.Enum)
			case telemetryfields.FieldInts:
				g.Ints = slices.Clone(f.Ints)
			case telemetryfields.FieldFloats:
				g.Floats = slices.Clone(f.Floats)
			case telemetryfields.FieldEnums:
				g.Enums = make([]string, len(f.Enums))
				for j, s := range f.Enums {
					g.Enums[j] = strings.Clone(s)
				}
			case telemetryfields.FieldObject:
				g.Object = clone(f.Object)
			}
			out[i] = g
		}
		return reflect.ValueOf(out)
	}
	switch v.Kind() {
	case reflect.String:
		out := reflect.New(v.Type()).Elem()
		out.SetString(strings.Clone(v.String()))
		return out
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(cloneValue(v.Elem()))
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			out.Field(i).Set(cloneValue(v.Field(i)))
		}
		return out
	case reflect.Slice:
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(cloneValue(v.Index(i)))
		}
		return out
	default:
		return v
	}
}
func retained(v reflect.Value) int64 {
	n := int64(v.Type().Size())
	if v.Type() == reflect.TypeFor[time.Time]() {
		return n
	}
	switch v.Kind() {
	case reflect.String:
		n += int64((v.Len() + 15) &^ 15)
	case reflect.Pointer:
		if !v.IsNil() {
			n += retained(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			n += retained(v.Field(i)) - int64(v.Field(i).Type().Size())
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			n += retained(v.Index(i))
		}
	}
	return n
}

func entityState(state RecordState, end time.Time) bool {
	if state == StateFinal {
		return !end.IsZero()
	}
	return end.IsZero()
}
