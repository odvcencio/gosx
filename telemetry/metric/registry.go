// Package metric provides a bounded, finite-domain telemetry registry.
package metric

import (
	"sync"

	"m31labs.dev/gosx/internal/telemetryauthority"
	"m31labs.dev/gosx/internal/telemetryerr"
)

var (
	ErrInvalidOptions = telemetryerr.ErrInvalidOptions
	ErrAfterBuild     = telemetryerr.ErrAfterBuild
	ErrCapacity       = telemetryerr.ErrCapacity
	ErrConflict       = telemetryerr.ErrConflict
)

type ConfigError = telemetryerr.ConfigError

type RegistryOptions struct {
	MaxSeries int
	MaxBytes  int64
}

const (
	defaultMaxSeries  = 32768
	defaultMaxBytes   = 8 << 20
	maxFamilies       = 256
	registryBaseBytes = 8192
)

// Registry reserves scalar samples and retained bytes before publishing any
// descriptor or tuple. The zero value lazily uses finite defaults. A nil
// registry validates descriptors but returns inert instruments.
type Registry struct {
	once       sync.Once
	state      *registryState
	privileged bool
}

type registryState struct {
	mu               sync.Mutex
	opts             RegistryOptions
	families         map[string]*family
	ordered          []*family
	samples          int
	bytes            int64
	sealed           bool
	snapshotGate     chan struct{}
	snapshotWaiters  int
	snapshotFamilies []FamilySnapshot
	textScratch      [4096]byte
}

func NewRegistry(opts RegistryOptions) (*Registry, error) {
	if opts.MaxSeries < 0 {
		return nil, invalid("max_series", "negative")
	}
	if opts.MaxBytes < 0 {
		return nil, invalid("max_bytes", "negative")
	}
	if opts.MaxSeries == 0 {
		opts.MaxSeries = defaultMaxSeries
	}
	if opts.MaxBytes == 0 {
		opts.MaxBytes = defaultMaxBytes
	}
	if opts.MaxBytes < registryBaseBytes {
		return nil, ErrCapacity
	}
	return &Registry{state: newState(opts)}, nil
}

func newState(opts RegistryOptions) *registryState {
	return &registryState{opts: opts, families: make(map[string]*family), bytes: registryBaseBytes, snapshotGate: make(chan struct{}, 1)}
}

func (r *Registry) get() *registryState {
	if r == nil {
		return nil
	}
	r.once.Do(func() {
		if r.state == nil {
			r.state = newState(RegistryOptions{MaxSeries: defaultMaxSeries, MaxBytes: defaultMaxBytes})
		}
	})
	return r.state
}

// WithAuthority creates a framework registration view over the same state.
// Application code cannot obtain a valid internal capability; public views
// always reject the reserved gosx_ prefix, including existing families.
func WithAuthority(r *Registry, key telemetryauthority.Key) (*Registry, error) {
	if !key.Valid() {
		return nil, invalid("registry_authority", "invalid")
	}
	if r == nil {
		return nil, invalid("registry", "required")
	}
	return &Registry{state: r.get(), privileged: true}, nil
}

// Seal closes registration and tuple declaration. Existing Bind calls become
// lookup-only; instruments already bound remain writable.
func (r *Registry) Seal() {
	if s := r.get(); s != nil {
		s.mu.Lock()
		s.sealed = true
		s.mu.Unlock()
	}
}

// Usage reports reserved framework memory and exposed scalar sample lines.
// It does not measure process RSS or application-owned instrument references.
type Usage struct {
	Families, Samples int
	Bytes             int64
	Sealed            bool
}

func (r *Registry) Usage() Usage {
	s := r.get()
	if s == nil {
		return Usage{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return Usage{len(s.families), s.samples, s.bytes, s.sealed}
}

func invalid(field, code string) error { return &ConfigError{Field: field, Code: code} }
