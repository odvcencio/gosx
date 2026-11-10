package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fillBatch returns a batch of puts whose encoded frame payload is exactly n
// bytes, and the keys it puts. Values are up to 1 MiB, the default limit.
func fillBatch(t *testing.T, prefix string, n int) (*Batch, []string) {
	t.Helper()
	b := &Batch{}
	var keys []string
	val := make([]byte, 1<<20)
	for i := range val {
		val[i] = byte(i)
	}
	total := 0
	for i := 0; total < n; i++ {
		key := fmt.Sprintf("%s-%04d", prefix, i)
		size := min(len(val), n-total-opBytes(op{key: key}))
		for size > 0 && opBytes(op{key: key, val: val[:size]}) > n-total {
			size--
		}
		b.Put(key, val[:size])
		keys = append(keys, key)
		total += opBytes(op{key: key, val: val[:size]})
	}
	if total != n {
		t.Fatalf("filled %d bytes, want %d", total, n)
	}
	return b, keys
}

func walPath(t *testing.T, dir string) string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "wal-*.log"))
	if len(m) != 1 {
		t.Fatalf("wal files %v", m)
	}
	return m[0]
}

// A batch whose frame is exactly the frame limit commits, and it and a later
// commit survive a reopen, both from the WAL and from a snapshot.
func TestBatchAtFrameLimitSurvivesReopen(t *testing.T) {
	for _, tc := range []struct {
		name      string
		compactAt int64
	}{{"wal", 1 << 40}, {"snapshot", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			o := Options{Sync: SyncNever, CompactAt: tc.compactAt}
			s := open(t, dir, o)
			b, keys := fillBatch(t, "at", MaxBatchBytes)
			must(t, s.Commit(b))
			must(t, s.Put("after", []byte("1")))
			must(t, s.Close())

			s = open(t, dir, o)
			defer s.Close()
			if got := s.Stats().TruncatedBytes; got != 0 {
				t.Fatalf("reopen truncated %d bytes", got)
			}
			for _, k := range keys {
				if _, ok := s.Get(k); !ok {
					t.Fatalf("%s lost after reopen", k)
				}
			}
			if v, ok := s.Get("after"); !ok || string(v) != "1" {
				t.Fatalf("commit after the large batch lost: %q %v", v, ok)
			}
		})
	}
}

// A batch one byte over the frame limit is refused before anything is
// written, and a later commit survives a reopen. Before the fix the batch was
// acknowledged, and Open dropped it and every later commit as a torn tail.
func TestBatchOverFrameLimitRejected(t *testing.T) {
	putsOver, _ := fillBatch(t, "over", MaxBatchBytes+1)
	// Deletes carry no data, so only the frame check can refuse them.
	deletesOver := &Batch{}
	key := strings.Repeat("d", 500)
	for n := 0; n <= MaxBatchBytes; n += opBytes(op{key: key}) {
		deletesOver.Delete(key)
	}
	for _, tc := range []struct {
		name  string
		batch *Batch
	}{{"puts", putsOver}, {"deletes", deletesOver}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			o := Options{Sync: SyncNever, CompactAt: 1 << 40}
			s := open(t, dir, o)
			must(t, s.Put("before", []byte("1")))
			walBefore := s.Stats().WALBytes
			keysBefore := s.Stats().Keys

			if err := s.Commit(tc.batch); !errors.Is(err, ErrBatchTooLarge) {
				t.Fatalf("Commit of an over-limit batch: %v, want ErrBatchTooLarge", err)
			}
			if st := s.Stats(); st.WALBytes != walBefore || st.Keys != keysBefore {
				t.Fatalf("rejected batch changed the store: wal %d->%d keys %d->%d", walBefore, st.WALBytes, keysBefore, st.Keys)
			}
			if fi, err := os.Stat(walPath(t, dir)); err != nil || fi.Size() != walBefore {
				t.Fatalf("rejected batch reached the WAL file: %v %v", fi, err)
			}
			must(t, s.Put("after", []byte("2")))
			must(t, s.Close())

			s = open(t, dir, o)
			defer s.Close()
			if got := s.Stats().TruncatedBytes; got != 0 {
				t.Fatalf("reopen truncated %d bytes", got)
			}
			for k, want := range map[string]string{"before": "1", "after": "2"} {
				if v, ok := s.Get(k); !ok || string(v) != want {
					t.Fatalf("%s = %q %v after reopen, want %q", k, v, ok, want)
				}
			}
			if got := s.Keys("over-"); len(got) != 0 {
				t.Fatalf("rejected keys present: %d", len(got))
			}
		})
	}
}

// Options cannot raise the key or value limit past what one frame holds,
// and the largest key and value allowed survive a snapshot next to other
// data.
func TestKeyAndValueLimitsCappedToFrame(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, Options{Sync: SyncNever, MaxValueBytes: 1 << 30, MaxKeyBytes: 1 << 30})
	if s.opts.MaxValueBytes != MaxValueLimit || s.opts.MaxKeyBytes != MaxKeyLimit {
		t.Fatalf("limits %d/%d, want capped to %d/%d", s.opts.MaxKeyBytes, s.opts.MaxValueBytes, MaxKeyLimit, MaxValueLimit)
	}
	if err := s.Put("k", make([]byte, MaxValueLimit+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("value over MaxValueLimit: %v, want ErrTooLarge", err)
	}
	if err := s.Put(strings.Repeat("k", MaxKeyLimit+1), nil); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("key over MaxKeyLimit: %v, want ErrTooLarge", err)
	}
	bigKey := strings.Repeat("k", MaxKeyLimit)
	must(t, s.Put("small", make([]byte, 200<<10)))
	must(t, s.Put(bigKey, make([]byte, MaxValueLimit)))
	if _, err := s.Append("log", make([]byte, MaxValueLimit)); err != nil {
		t.Fatal(err)
	}
	must(t, s.Compact())
	must(t, s.Close())

	s = open(t, dir, Options{})
	defer s.Close()
	if v, ok := s.Get(bigKey); !ok || len(v) != MaxValueLimit {
		t.Fatalf("largest value after snapshot reopen: %d bytes, %v", len(v), ok)
	}
	if v, ok := s.Get("small"); !ok || len(v) != 200<<10 {
		t.Fatalf("small value after snapshot reopen: %d bytes, %v", len(v), ok)
	}
	if e := s.LogRead("log", 1, 0); len(e) != 1 || len(e[0].Data) != MaxValueLimit {
		t.Fatalf("largest log record after snapshot reopen: %d entries", len(e))
	}
}
