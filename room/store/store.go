package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Errors returned by the store.
var (
	ErrLocked   = errors.New("store: directory is locked by another process")
	ErrClosed   = errors.New("store: closed")
	ErrCorrupt  = errors.New("store: snapshot is corrupt")
	ErrFull     = errors.New("store: data size limit reached")
	ErrTooLarge = errors.New("store: key or value too large")
	ErrBadKey   = errors.New("store: empty key")
	// ErrBatchTooLarge refuses a batch whose encoded size is over
	// MaxBatchBytes. Nothing is written; split the batch.
	ErrBatchTooLarge = errors.New("store: batch too large for one commit")
)

// Upper bounds for Options.MaxKeyBytes and Options.MaxValueBytes. Together
// with snapshot chunking they keep every frame the store writes (WAL commits
// and snapshot chunks) under MaxBatchBytes, so recovery never mistakes a
// frame for a torn tail.
const (
	MaxKeyLimit   = 64 << 10
	MaxValueLimit = 32 << 20
)

// SyncMode says when a commit reaches stable storage.
type SyncMode int

const (
	// SyncAlways fsyncs the WAL before every commit returns (default).
	SyncAlways SyncMode = iota
	// SyncNever leaves flushing to the operating system. A process crash
	// still loses nothing; a machine crash can lose recent commits.
	SyncNever
)

// Options configures Open. Zero values select the defaults.
type Options struct {
	Sync SyncMode
	// CompactAt is the WAL size, in bytes, that triggers a compaction.
	// Default 4 MiB.
	CompactAt int64
	// MaxLogEntries keeps this many newest entries in each append log; older
	// entries are dropped. Default 10000.
	MaxLogEntries int
	// MaxValueBytes limits one value or log record. Default 1 MiB; at most
	// MaxValueLimit (32 MiB). A whole batch is limited to MaxBatchBytes.
	MaxValueBytes int
	// MaxKeyBytes limits one key or log name. Default 512; at most
	// MaxKeyLimit (64 KiB).
	MaxKeyBytes int
	// MaxDataBytes limits live data (keys, values, retained log records).
	// Default 256 MiB.
	MaxDataBytes int64
	// LockTimeout makes Open wait this long for another process to release
	// the directory lock, for rollouts where the old pod is still stopping.
	// Default 0: fail at once with ErrLocked.
	LockTimeout time.Duration

	// hook runs at named steps of a compaction; a non-nil error aborts it as
	// if the process died there. For crash tests.
	hook func(step string) error
}

func (o *Options) defaults() {
	if o.CompactAt <= 0 {
		o.CompactAt = 4 << 20
	}
	if o.MaxLogEntries <= 0 {
		o.MaxLogEntries = 10000
	}
	if o.MaxValueBytes <= 0 {
		o.MaxValueBytes = 1 << 20
	}
	o.MaxValueBytes = min(o.MaxValueBytes, MaxValueLimit)
	if o.MaxKeyBytes <= 0 {
		o.MaxKeyBytes = 512
	}
	o.MaxKeyBytes = min(o.MaxKeyBytes, MaxKeyLimit)
	if o.MaxDataBytes <= 0 {
		o.MaxDataBytes = 256 << 20
	}
}

// Stats describes the store for logs and health checks.
type Stats struct {
	Generation     uint64
	Keys           int
	Logs           int
	DataBytes      int64
	WALBytes       int64
	Compactions    int
	TruncatedBytes int64 // torn-tail bytes discarded by Open
	RecoveredBatch int   // batches replayed from the WAL by Open
	LastCompactErr error
}

// Entry is one record of an append log.
type Entry struct {
	Seq  uint64
	Data []byte
}

// Store is a durable key-value store with append logs. It is safe for
// concurrent use by one process.
type Store struct {
	dir  string
	opts Options

	mu      sync.RWMutex
	st      *state
	gen     uint64
	wal     *os.File
	walSize int64
	lock    *os.File
	closed  bool
	broken  error
	stats   Stats
	buf     []byte
	// retryAt delays the next compaction attempt after a failed one.
	retryAt int64
}

const snapMagic = "GXSNAP1\n"

func snapName(gen uint64) string { return fmt.Sprintf("snap-%020d.snap", gen) }
func walName(gen uint64) string  { return fmt.Sprintf("wal-%020d.log", gen) }

// Open opens or creates the store in dir and recovers it from disk.
func Open(dir string, opts Options) (*Store, error) {
	opts.defaults()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, opts: opts}
	if err := s.acquireLock(); err != nil {
		return nil, err
	}
	if err := s.recover(); err != nil {
		s.releaseLock()
		return nil, err
	}
	return s, nil
}

func (s *Store) acquireLock() error {
	f, err := os.OpenFile(filepath.Join(s.dir, "LOCK"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(s.opts.LockTimeout)
	for {
		err = tryLock(f)
		if err == nil {
			s.lock = f
			return nil
		}
		if err != ErrLocked || !time.Now().Before(deadline) {
			f.Close()
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (s *Store) releaseLock() {
	if s.lock != nil {
		s.lock.Close() // closing drops the flock
		s.lock = nil
	}
}

type genFiles struct {
	snaps, wals []uint64
}

func listGens(dir string) (genFiles, error) {
	var g genFiles
	ents, err := os.ReadDir(dir)
	if err != nil {
		return g, err
	}
	for _, e := range ents {
		name := e.Name()
		switch {
		case strings.HasSuffix(name, ".tmp"):
			_ = os.Remove(filepath.Join(dir, name)) // interrupted compaction
		case strings.HasPrefix(name, "snap-") && strings.HasSuffix(name, ".snap"):
			if n, err := strconv.ParseUint(name[5:len(name)-5], 10, 64); err == nil {
				g.snaps = append(g.snaps, n)
			}
		case strings.HasPrefix(name, "wal-") && strings.HasSuffix(name, ".log"):
			if n, err := strconv.ParseUint(name[4:len(name)-4], 10, 64); err == nil {
				g.wals = append(g.wals, n)
			}
		}
	}
	sort.Slice(g.snaps, func(i, j int) bool { return g.snaps[i] < g.snaps[j] })
	sort.Slice(g.wals, func(i, j int) bool { return g.wals[i] < g.wals[j] })
	return g, nil
}

func (s *Store) recover() error {
	g, err := listGens(s.dir)
	if err != nil {
		return err
	}
	s.st = newState(s.opts.MaxLogEntries)
	var gen uint64
	if n := len(g.snaps); n > 0 {
		gen = g.snaps[n-1]
		data, err := os.ReadFile(filepath.Join(s.dir, snapName(gen)))
		if err != nil {
			return err
		}
		if err := loadSnapshot(data, s.st); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrCorrupt, snapName(gen), err)
		}
	} else if n := len(g.wals); n > 0 {
		gen = g.wals[n-1] // generation 0 has no snapshot
		if gen != 0 {
			return fmt.Errorf("%w: wal-%d has no snapshot", ErrCorrupt, gen)
		}
	}
	walPath := filepath.Join(s.dir, walName(gen))
	f, err := os.OpenFile(walPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(walPath)
	if err != nil {
		f.Close()
		return err
	}
	valid, frames, err := scanLog(data, func(ops []op) error { s.st.applyAll(ops); return nil })
	if err != nil {
		f.Close()
		return err
	}
	if valid < len(data) {
		s.stats.TruncatedBytes = int64(len(data) - valid)
		if err := f.Truncate(int64(valid)); err != nil {
			f.Close()
			return err
		}
		if err := f.Sync(); err != nil {
			f.Close()
			return err
		}
	}
	if _, err := f.Seek(int64(valid), 0); err != nil {
		f.Close()
		return err
	}
	s.wal, s.walSize, s.gen = f, int64(valid), gen
	s.stats.RecoveredBatch = frames
	// Older generations are leftovers of a crash between the rename of the new
	// snapshot and the removal of the old files.
	for _, n := range g.snaps {
		if n < gen {
			_ = os.Remove(filepath.Join(s.dir, snapName(n)))
		}
	}
	for _, n := range g.wals {
		if n < gen {
			_ = os.Remove(filepath.Join(s.dir, walName(n)))
		}
	}
	return syncDir(s.dir)
}

func loadSnapshot(data []byte, st *state) error {
	if len(data) < len(snapMagic) || string(data[:len(snapMagic)]) != snapMagic {
		return errors.New("bad magic")
	}
	body := data[len(snapMagic):]
	var count, want uint64
	ended := false
	valid, _, err := scanLog(body, func(ops []op) error {
		for _, o := range ops {
			if ended {
				return errors.New("data after end marker")
			}
			if o.kind == opEnd {
				v, w := decodeUvarint(o.val)
				if w <= 0 {
					return errors.New("bad end marker")
				}
				want, ended = v, true
				continue
			}
			count++
			st.apply(o)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if valid != len(body) || !ended || count != want {
		return errors.New("incomplete snapshot")
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	return nil
}

// Close flushes and closes the store and releases the directory lock.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var err error
	if s.wal != nil {
		err = s.wal.Sync()
		if cerr := s.wal.Close(); err == nil {
			err = cerr
		}
		s.wal = nil
	}
	s.releaseLock()
	return err
}

// Stats returns a snapshot of the store's counters.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.stats
	st.Generation = s.gen
	st.Keys = len(s.st.kv)
	st.Logs = len(s.st.logs)
	st.DataBytes = s.st.bytes
	st.WALBytes = s.walSize
	return st
}

// Get returns a copy of the value stored at key.
func (s *Store) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.st.kv[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), v...), true
}

// Keys returns the keys that start with prefix, sorted.
func (s *Store) Keys(prefix string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var keys []string
	for k := range s.st.kv {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// Scan calls fn for each key with prefix in sorted order until fn returns
// false. fn must not call back into the store, and must copy val to keep it.
func (s *Store) Scan(prefix string, fn func(key string, val []byte) bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var keys []string
	for k := range s.st.kv {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !fn(k, s.st.kv[k]) {
			return
		}
	}
}

// LogRange returns the sequence of the oldest retained entry of the log and
// the sequence the next Append will get. Sequences start at 1.
func (s *Store) LogRange(name string) (first, next uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l := s.st.logs[name]
	if l == nil {
		return 1, 1
	}
	return l.first, l.first + uint64(len(l.entries))
}

// LogRead returns up to limit retained entries with sequence >= from (limit
// <= 0: no limit).
func (s *Store) LogRead(name string, from uint64, limit int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l := s.st.logs[name]
	if l == nil {
		return nil
	}
	if from < l.first {
		from = l.first
	}
	var out []Entry
	for seq := from; seq < l.first+uint64(len(l.entries)); seq++ {
		if limit > 0 && len(out) == limit {
			break
		}
		out = append(out, Entry{Seq: seq, Data: append([]byte(nil), l.entries[seq-l.first]...)})
	}
	return out
}

// Batch collects changes that commit together or not at all.
type Batch struct {
	ops []op
	err error
}

// Put sets key to val.
func (b *Batch) Put(key string, val []byte) *Batch {
	b.ops = append(b.ops, op{kind: opPut, key: key, val: append([]byte(nil), val...)})
	return b
}

// Delete removes key.
func (b *Batch) Delete(key string) *Batch {
	b.ops = append(b.ops, op{kind: opDel, key: key})
	return b
}

// Append adds rec to the named log.
func (b *Batch) Append(log string, rec []byte) *Batch {
	b.ops = append(b.ops, op{kind: opAppend, key: log, val: append([]byte(nil), rec...)})
	return b
}

// DropLog deletes the named append log and its sequence numbers.
func (b *Batch) DropLog(log string) *Batch {
	b.ops = append(b.ops, op{kind: opDropLog, key: log})
	return b
}

// Commit makes the batch durable and visible. On error nothing changed.
func (s *Store) Commit(b *Batch) error {
	if b == nil || len(b.ops) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.commitLocked(b.ops)
	return err
}

// Put sets key to val durably.
func (s *Store) Put(key string, val []byte) error {
	return s.Commit((&Batch{}).Put(key, val))
}

// Delete removes key durably.
func (s *Store) Delete(key string) error {
	return s.Commit((&Batch{}).Delete(key))
}

// Append adds rec to the named log and returns its sequence number.
func (s *Store) Append(log string, rec []byte) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commitLocked([]op{{kind: opAppend, key: log, val: rec}})
}

func (s *Store) commitLocked(ops []op) (uint64, error) {
	if s.closed {
		return 0, ErrClosed
	}
	if s.broken != nil {
		return 0, s.broken
	}
	var delta int64
	frame := 0
	seen := map[string]bool{}
	for _, o := range ops {
		if o.key == "" {
			return 0, ErrBadKey
		}
		if len(o.key) > s.opts.MaxKeyBytes || len(o.val) > s.opts.MaxValueBytes {
			return 0, ErrTooLarge
		}
		// Recovery reads a frame over maxFrame as a torn tail and drops it
		// with every later commit, so such a batch must never be written.
		if frame += opBytes(o); frame > maxFrame {
			return 0, ErrBatchTooLarge
		}
		switch o.kind {
		case opPut:
			delta += int64(len(o.key) + len(o.val))
			if old, ok := s.st.kv[o.key]; ok && !seen["k"+o.key] {
				delta -= int64(len(o.key) + len(old))
			}
			seen["k"+o.key] = true
		case opDropLog:
			if l := s.st.logs[o.key]; l != nil {
				for _, e := range l.entries {
					delta -= int64(len(e)) + entryOverhead
				}
			}
		case opDel:
			if old, ok := s.st.kv[o.key]; ok && !seen["k"+o.key] {
				delta -= int64(len(o.key) + len(old))
			}
			seen["k"+o.key] = true
		case opAppend:
			delta += int64(len(o.val)) + entryOverhead
		default:
			return 0, errOps
		}
	}
	if delta > 0 && s.st.bytes+delta > s.opts.MaxDataBytes {
		return 0, ErrFull
	}
	s.buf = appendFrame(s.buf[:0], ops)
	if _, err := s.wal.Write(s.buf); err != nil {
		s.rollbackWAL(err)
		return 0, err
	}
	if s.opts.Sync == SyncAlways {
		if err := s.wal.Sync(); err != nil {
			s.rollbackWAL(err)
			return 0, err
		}
	}
	s.walSize += int64(len(s.buf))
	var seq uint64
	for _, o := range ops {
		s.st.apply(o)
		if o.kind == opAppend {
			l := s.st.logs[o.key]
			seq = l.first + uint64(len(l.entries)) - 1
		}
	}
	if s.walSize >= s.opts.CompactAt && s.walSize >= s.retryAt {
		s.stats.LastCompactErr = s.compactLocked()
		if s.stats.LastCompactErr != nil {
			s.retryAt = s.walSize + s.opts.CompactAt
		}
	}
	return seq, nil
}

// rollbackWAL removes a partial frame after a failed write. If it cannot, the
// store refuses further writes until it is reopened (Open truncates the tail).
func (s *Store) rollbackWAL(cause error) {
	if err := s.wal.Truncate(s.walSize); err != nil {
		s.broken = fmt.Errorf("store: wal write failed (%v) and rollback failed: %w", cause, err)
		return
	}
	if _, err := s.wal.Seek(s.walSize, 0); err != nil {
		s.broken = fmt.Errorf("store: wal write failed (%v) and seek failed: %w", cause, err)
	}
}

// Compact writes a snapshot of the current state and starts a fresh WAL.
func (s *Store) Compact() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if s.broken != nil {
		return s.broken
	}
	return s.compactLocked()
}

func (s *Store) hook(step string) error {
	if s.opts.hook != nil {
		return s.opts.hook(step)
	}
	return nil
}

func (s *Store) compactLocked() error {
	next := s.gen + 1
	tmp := filepath.Join(s.dir, snapName(next)+".tmp")
	final := filepath.Join(s.dir, snapName(next))
	if err := s.hook("snapshot-start"); err != nil {
		return err
	}
	if err := writeSnapshot(tmp, s.st, s.opts.Sync == SyncAlways); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := s.hook("snapshot-written"); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// From here the new generation is the truth on disk; any failure must stop
	// writes to the old WAL, which recovery would ignore.
	fail := func(err error) error {
		s.broken = fmt.Errorf("store: compaction failed after snapshot rename: %w", err)
		return s.broken
	}
	if err := s.hook("snapshot-renamed"); err != nil {
		return fail(err)
	}
	if err := syncDir(s.dir); err != nil {
		return fail(err)
	}
	nw, err := os.OpenFile(filepath.Join(s.dir, walName(next)), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fail(err)
	}
	if err := syncDir(s.dir); err != nil {
		nw.Close()
		return fail(err)
	}
	if err := s.hook("wal-created"); err != nil {
		nw.Close()
		return fail(err)
	}
	old := s.gen
	_ = s.wal.Close()
	s.wal, s.walSize, s.gen = nw, 0, next
	_ = os.Remove(filepath.Join(s.dir, walName(old)))
	_ = os.Remove(filepath.Join(s.dir, snapName(old)))
	_ = syncDir(s.dir)
	s.stats.Compactions++
	s.retryAt = 0
	return nil
}

func writeSnapshot(path string, st *state, doSync bool) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
		}
	}()
	var out []byte
	out = append(out, snapMagic...)
	var chunk []op
	var chunkBytes int
	var count uint64
	flush := func() error {
		if len(chunk) == 0 {
			return nil
		}
		out = appendFrame(out, chunk)
		chunk, chunkBytes = chunk[:0], 0
		if len(out) >= 1<<20 {
			if _, err := f.Write(out); err != nil {
				return err
			}
			out = out[:0]
		}
		return nil
	}
	// A frame holds up to 256 KiB of ops, or one larger op on its own, so no
	// snapshot frame is bigger than the largest op (at most MaxBatchBytes).
	add := func(o op) error {
		n := opBytes(o)
		if len(chunk) > 0 && chunkBytes+n > 256<<10 {
			if err := flush(); err != nil {
				return err
			}
		}
		chunk = append(chunk, o)
		chunkBytes += n
		count++
		return nil
	}
	for _, k := range sortedKeys(st.kv) {
		if err := add(op{kind: opPut, key: k, val: st.kv[k]}); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(st.logs))
	for n := range st.logs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		l := st.logs[n]
		if err := add(op{kind: opLogHead, key: n, val: encodeUvarint(l.first)}); err != nil {
			return err
		}
		for _, e := range l.entries {
			if err := add(op{kind: opAppend, key: n, val: e}); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	out = appendFrame(out, []op{{kind: opEnd, val: encodeUvarint(count)}})
	if _, err := f.Write(out); err != nil {
		return err
	}
	if doSync {
		if err := f.Sync(); err != nil {
			return err
		}
	}
	ok = true
	return f.Close()
}

func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
