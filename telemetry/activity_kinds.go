package telemetry

import (
	"encoding/hex"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"m31labs.dev/gosx/telemetry/schema"
)

type Dimension struct {
	Name   string
	Values []string
}

type ActivityKindOptions struct {
	Dimensions                       []Dimension
	Combinations                     [][2]string
	Outcomes, Reasons, Events, Roles []string
}

type activityState struct {
	mu              sync.Mutex
	kinds           map[string]*activityKindCore
	pool            *fieldPool
	bytes           atomic.Int64
	live            map[string]*activityEntity
	attached        map[*Loop]*activityEntity
	entropy         sync.Mutex
	transactions    atomic.Int32
	stopping        atomic.Bool
	sequence        uint64
	stream, boot    string
	lastMaintenance time.Duration
	known           [256]string
	nextKnown       int
	events          *recordQueue
	eventPublish    sync.Mutex
}

type activityKindCore struct {
	owner                            *Telemetry
	name                             string
	dimensions                       []Dimension
	combinations                     [][2]string
	outcomes, reasons, events, roles []string
	meters                           activityMeters
	open                             int64 // activityState.mu
}

// ActivityKind owns a startup declaration of one finite activity shape. Its
// codecs are retained for the telemetry lifetime; avoid capturing game state.
// Register kinds before Build; Begin creates an activity with copied fields.
type ActivityKind[A, P, E any] struct {
	core        *activityKindCore
	activity    *compiledDomainCodec[A]
	participant *compiledDomainCodec[P]
	event       *compiledDomainCodec[E]
}

func (t *Telemetry) initializeActivities() error {
	if !t.opts.Activities.Disabled && t.opts.Limits.MaxQueuedBytes < recordQueueMetadataBytes(t.opts.Limits.MaxQueuedRecords)+int64(t.opts.Activities.MaxRecordBytes)+256 {
		return invalid("queue_bytes", "incompatible_reservations")
	}
	const bytes = fieldPoolBytes + 64<<10
	if !t.reserveMisc(bytes) {
		return ErrCapacity
	}
	s := &activityState{kinds: make(map[string]*activityKindCore, 32), pool: new(fieldPool),
		live: make(map[string]*activityEntity, t.opts.Activities.MaxOpen), attached: make(map[*Loop]*activityEntity, t.opts.Activities.MaxOpen)}
	if !t.opts.Activities.Disabled {
		s.events = newRecordQueue(t.opts.Limits.MaxQueuedRecords, t.opts.Limits.MaxQueuedBytes)
		if s.events == nil {
			return invalid("queue_bytes", "incompatible_reservations")
		}
	}
	s.boot = hex.EncodeToString(t.boot[:])
	s.stream = s.boot
	s.bytes.Store(bytes)
	t.activities = s
	return nil
}

func validateActivityNames(values []string, limit int) error {
	if len(values) > limit {
		return invalid("activity_kind", "capacity")
	}
	for i, value := range values {
		if !kindName(value) {
			return invalid("activity_kind", "name")
		}
		for _, old := range values[:i] {
			if old == value {
				return invalid("activity_kind", "duplicate")
			}
		}
	}
	return nil
}

func prepareActivityOptions(opts ActivityKindOptions) (ActivityKindOptions, int64, error) {
	if len(opts.Dimensions) > 2 || len(opts.Combinations) > 64 {
		return opts, 0, invalid("activity_kind", "capacity")
	}
	for _, list := range []struct {
		values []string
		limit  int
	}{
		{opts.Outcomes, 16}, {opts.Reasons, 32}, {opts.Events, 64}, {opts.Roles, 8},
	} {
		if err := validateActivityNames(list.values, list.limit); err != nil {
			return opts, 0, err
		}
	}
	values := [2][]string{{"none"}, {"none"}}
	for i, d := range opts.Dimensions {
		if !kindName(d.Name) || len(d.Values) == 0 {
			return opts, 0, invalid("activity_dimension", "descriptor")
		}
		if i == 1 && d.Name == opts.Dimensions[0].Name {
			return opts, 0, invalid("activity_dimension", "duplicate")
		}
		if err := validateHubValues(d.Values, 16); err != nil {
			return opts, 0, err
		}
		for _, value := range d.Values {
			if len(value) > 64 {
				return opts, 0, invalid("activity_dimension", "text")
			}
		}
		values[i] = d.Values
	}
	if len(opts.Combinations) == 0 {
		if len(values[0])*len(values[1]) > 64 {
			return opts, 0, invalid("activity_combinations", "capacity")
		}
		combos := make([][2]string, 0, len(values[0])*len(values[1]))
		for _, a := range values[0] {
			for _, b := range values[1] {
				combos = append(combos, [2]string{a, b})
			}
		}
		opts.Combinations = combos
	} else {
		for i, combo := range opts.Combinations {
			for slot, value := range combo {
				found := false
				for _, declared := range values[slot] {
					found = found || value == declared
				}
				if !found {
					return opts, 0, invalid("activity_combinations", "undeclared")
				}
			}
			for _, old := range opts.Combinations[:i] {
				if old == combo {
					return opts, 0, invalid("activity_combinations", "duplicate")
				}
			}
		}
	}
	// Finite kind metadata, lookup headers and future prebound instruments.
	charge := int64(16 << 10)
	for _, d := range opts.Dimensions {
		charge += int64(64 + len(d.Name))
		for _, v := range d.Values {
			charge += int64(32 + len(v))
		}
	}
	for _, combo := range opts.Combinations {
		charge += int64(128 + len(combo[0]) + len(combo[1]))
	}
	for _, list := range [][]string{opts.Outcomes, opts.Reasons, opts.Events, opts.Roles} {
		for _, value := range list {
			charge += int64(64 + len(value))
		}
	}
	return opts, charge, nil
}

func cloneActivityValues(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = strings.Clone(v)
	}
	return out
}

func freezeActivityKind(t *Telemetry, name string, opts ActivityKindOptions) *activityKindCore {
	k := &activityKindCore{owner: t, name: strings.Clone(name), outcomes: cloneActivityValues(opts.Outcomes), reasons: cloneActivityValues(opts.Reasons), events: cloneActivityValues(opts.Events), roles: cloneActivityValues(opts.Roles)}
	k.dimensions = make([]Dimension, len(opts.Dimensions))
	for i, d := range opts.Dimensions {
		k.dimensions[i] = Dimension{Name: strings.Clone(d.Name), Values: cloneActivityValues(d.Values)}
	}
	k.combinations = make([][2]string, len(opts.Combinations))
	for i, c := range opts.Combinations {
		k.combinations[i] = [2]string{strings.Clone(c[0]), strings.Clone(c[1])}
	}
	return k
}

// newActivityKind validates all descriptors and atomically reserves the whole
// kind before route admission. Nil/disabled owners validate then return inert
// declarations. Neither failed registration nor Build admits a partial kind.
func newActivityKind[A, P, E any](t *Telemetry, name string, opts ActivityKindOptions, codecs ActivityCodecs[A, P, E]) (*ActivityKind[A, P, E], error) {
	if !kindName(name) {
		return nil, invalid("activity_kind", "name")
	}
	opts, charge, err := prepareActivityOptions(opts)
	if err != nil {
		return nil, err
	}
	a, ab, ac, ae, err := prepareDomainCodec(codecs.Activity)
	if err != nil {
		return nil, err
	}
	p, pb, pc, pe, err := prepareDomainCodec(codecs.Participant)
	if err != nil {
		return nil, err
	}
	e, eb, ec, ee, err := prepareDomainCodec(codecs.Event)
	if err != nil {
		return nil, err
	}
	charge += ab + pb + eb + int64(len(name))
	if t == nil || t.activities == nil || t.opts.Activities.Disabled {
		return &ActivityKind[A, P, E]{}, nil
	}
	s := t.activities
	s.mu.Lock()
	defer s.mu.Unlock()
	if !t.Enabled() {
		return nil, ErrClosed
	}
	if t.registry.Usage().Sealed {
		return nil, ErrAfterBuild
	}
	if s.kinds[name] != nil {
		return nil, ErrConflict
	}
	if len(s.kinds) == 32 {
		t.core.dropped["activity_cap"].Add(1)
		return nil, ErrCapacity
	}
	if !t.reserveMisc(charge) {
		t.core.dropped["memory"].Add(1)
		return nil, ErrCapacity
	}
	names := [2]string{"none", "none"}
	for i, d := range opts.Dimensions {
		names[i] = d.Name
	}
	if err = t.bindActivityMetrics(name, names, opts.Combinations, opts.Outcomes, opts.Reasons); err != nil {
		t.releaseMisc(charge)
		return nil, err
	}
	k := &ActivityKind[A, P, E]{core: freezeActivityKind(t, name, opts), activity: freezeDomainCodec(a, ab, ac, ae), participant: freezeDomainCodec(p, pb, pc, pe), event: freezeDomainCodec(e, eb, ec, ee)}
	k.core.bindActivityMeters()
	s.kinds[k.core.name] = k.core
	s.bytes.Add(charge)
	return k, nil
}

func (k *activityKindCore) dimensionTuple(values [2]string) [2]string {
	for i := len(k.dimensions); i < 2; i++ {
		values[i] = "none"
	}
	for _, tuple := range k.combinations {
		if tuple == values {
			return tuple
		}
	}
	return [2]string{"other", "other"}
}

func (k *activityKindCore) schemaDimensions(tuple [2]string) []schema.Dimension {
	out := make([]schema.Dimension, len(k.dimensions))
	for i, d := range k.dimensions {
		out[i] = schema.Dimension{Name: d.Name, Value: tuple[i]}
	}
	return out
}

// NewActivityKind registers a finite typed activity declaration before Build.
// Encoders must avoid capturing game/request state; they live with telemetry.
func NewActivityKind[A, P, E any](t *Telemetry, name string, opts ActivityKindOptions, codecs ActivityCodecs[A, P, E]) (*ActivityKind[A, P, E], error) {
	return newActivityKind(t, name, opts, codecs)
}
