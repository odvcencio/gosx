package store

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func open(t *testing.T, dir string, o Options) *Store {
	t.Helper()
	s, err := Open(dir, o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestKVRoundTripAndReopen(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, Options{})
	must(t, s.Put("a", []byte("1")))
	must(t, s.Put("b/x", []byte("2")))
	must(t, s.Put("b/y", []byte("3")))
	must(t, s.Delete("a"))
	if _, ok := s.Get("a"); ok {
		t.Fatal("a should be deleted")
	}
	if got := s.Keys("b/"); strings.Join(got, ",") != "b/x,b/y" {
		t.Fatalf("keys = %v", got)
	}
	v, _ := s.Get("b/x")
	v[0] = 'Z' // Get returns a copy
	if v2, _ := s.Get("b/x"); string(v2) != "2" {
		t.Fatalf("Get aliases stored value: %q", v2)
	}
	must(t, s.Close())

	s = open(t, dir, Options{})
	defer s.Close()
	if v, ok := s.Get("b/y"); !ok || string(v) != "3" {
		t.Fatalf("after reopen b/y = %q %v", v, ok)
	}
	if _, ok := s.Get("a"); ok {
		t.Fatal("deleted key came back")
	}
	if s.Stats().RecoveredBatch != 4 {
		t.Fatalf("recovered %d batches, want 4", s.Stats().RecoveredBatch)
	}
}

func TestBatchAtomicAndLogs(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, Options{})
	b := &Batch{}
	b.Put("k", []byte("v")).Append("log", []byte("one")).Append("log", []byte("two"))
	must(t, s.Commit(b))
	seq, err := s.Append("log", []byte("three"))
	must(t, err)
	if seq != 3 {
		t.Fatalf("seq = %d", seq)
	}
	// A batch with a bad op commits nothing.
	bad := (&Batch{}).Put("ok", []byte("x")).Put("", []byte("y"))
	if err := s.Commit(bad); !errors.Is(err, ErrBadKey) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := s.Get("ok"); ok {
		t.Fatal("partial batch applied")
	}
	must(t, s.Close())

	s = open(t, dir, Options{})
	defer s.Close()
	es := s.LogRead("log", 2, 0)
	if len(es) != 2 || es[0].Seq != 2 || string(es[1].Data) != "three" {
		t.Fatalf("entries = %+v", es)
	}
	if first, next := s.LogRange("log"); first != 1 || next != 4 {
		t.Fatalf("range = %d %d", first, next)
	}
}

func TestLogTrimKeepsSequences(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, Options{MaxLogEntries: 3})
	for i := 1; i <= 7; i++ {
		seq, err := s.Append("l", []byte(strconv.Itoa(i)))
		must(t, err)
		if seq != uint64(i) {
			t.Fatalf("seq %d != %d", seq, i)
		}
	}
	first, next := s.LogRange("l")
	if first != 5 || next != 8 {
		t.Fatalf("range %d..%d", first, next)
	}
	must(t, s.Compact())
	must(t, s.Close())
	s = open(t, dir, Options{MaxLogEntries: 3})
	defer s.Close()
	first, next = s.LogRange("l")
	if first != 5 || next != 8 {
		t.Fatalf("after compact+reopen range %d..%d", first, next)
	}
	seq, _ := s.Append("l", []byte("8"))
	if seq != 8 {
		t.Fatalf("seq continues at %d", seq)
	}
}

func TestEmptyLogKeepsSequenceAcrossSnapshot(t *testing.T) {
	// A log whose retained entries are all gone must not restart at 1.
	dir := t.TempDir()
	s := open(t, dir, Options{MaxLogEntries: 1})
	s.Append("l", []byte("a"))
	s.Append("l", []byte("b"))
	must(t, s.Compact())
	must(t, s.Close())
	s = open(t, dir, Options{MaxLogEntries: 1})
	defer s.Close()
	if seq, _ := s.Append("l", []byte("c")); seq != 3 {
		t.Fatalf("seq = %d, want 3", seq)
	}
}

func TestLimits(t *testing.T) {
	s := open(t, t.TempDir(), Options{MaxValueBytes: 8, MaxKeyBytes: 4, MaxDataBytes: 64})
	defer s.Close()
	if err := s.Put("toolong", []byte("x")); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("key: %v", err)
	}
	if err := s.Put("k", make([]byte, 9)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("value: %v", err)
	}
	for i := 0; ; i++ {
		err := s.Put(fmt.Sprintf("k%d", i), make([]byte, 8))
		if errors.Is(err, ErrFull) {
			break
		}
		if err != nil || i > 100 {
			t.Fatalf("i=%d err=%v", i, err)
		}
	}
	if s.Stats().DataBytes > 64 {
		t.Fatalf("data bytes %d over cap", s.Stats().DataBytes)
	}
	// Deleting frees room.
	must(t, s.Delete("k0"))
	must(t, s.Put("new", make([]byte, 4)))
}

func TestCompactionBoundsDisk(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, Options{CompactAt: 16 << 10, Sync: SyncNever, MaxLogEntries: 100})
	defer s.Close()
	val := bytes.Repeat([]byte("x"), 200)
	for i := 0; i < 5000; i++ {
		must(t, s.Put(fmt.Sprintf("k%d", i%50), val)) // 50 live keys, 5000 writes
		must(t, func() error { _, err := s.Append("l", val); return err }())
	}
	if s.Stats().Compactions == 0 {
		t.Fatal("never compacted")
	}
	var total int64
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		fi, _ := e.Info()
		total += fi.Size()
	}
	// The live set is 50 keys and 100 log records of 200 B; the 5000
	// overwrites must not pile up beyond snapshot + WAL.
	if limit := s.Stats().DataBytes*2 + 64<<10; total > limit {
		t.Fatalf("disk %d bytes exceeds %d (data %d)", total, limit, s.Stats().DataBytes)
	}
	if len(ents) > 4 {
		t.Fatalf("old generations not removed: %v", names(ents))
	}
}

func names(ents []os.DirEntry) []string {
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// Truncating the WAL at every byte offset must recover exactly the batches
// that fit whole before the cut.
func TestTornTailAtEveryOffset(t *testing.T) {
	src := t.TempDir()
	s := open(t, src, Options{})
	var ends []int64 // WAL size after each commit
	for i := 0; i < 12; i++ {
		b := (&Batch{}).Put("k"+strconv.Itoa(i), bytes.Repeat([]byte{byte(i)}, i*3+1)).Append("l", []byte{byte(i)})
		must(t, s.Commit(b))
		ends = append(ends, s.Stats().WALBytes)
	}
	must(t, s.Close())
	wal, err := os.ReadFile(filepath.Join(src, walName(0)))
	if err != nil {
		t.Fatal(err)
	}
	for cut := 0; cut <= len(wal); cut++ {
		dir := t.TempDir()
		must(t, os.WriteFile(filepath.Join(dir, walName(0)), wal[:cut], 0o644))
		r := open(t, dir, Options{Sync: SyncNever})
		want := 0
		for _, e := range ends {
			if int64(cut) >= e {
				want++
			}
		}
		if got := len(r.Keys("k")); got != want {
			t.Fatalf("cut %d: %d keys, want %d", cut, got, want)
		}
		if _, next := r.LogRange("l"); next != uint64(want)+1 {
			t.Fatalf("cut %d: log next %d, want %d", cut, next, want+1)
		}
		if cut%5 != 0 {
			r.Close()
			continue
		}
		// The store keeps working and the torn bytes are gone.
		must(t, r.Put("after", []byte("ok")))
		must(t, r.Close())
		r = open(t, dir, Options{})
		if v, _ := r.Get("after"); string(v) != "ok" {
			t.Fatalf("cut %d: write after recovery lost", cut)
		}
		r.Close()
	}
}

func TestCorruptTailTruncated(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, Options{})
	must(t, s.Put("a", []byte("1")))
	must(t, s.Put("b", []byte("2")))
	must(t, s.Close())
	p := filepath.Join(dir, walName(0))
	data, _ := os.ReadFile(p)
	data[len(data)-1] ^= 0xff // flip a bit in the last frame
	must(t, os.WriteFile(p, data, 0o644))
	r := open(t, dir, Options{})
	defer r.Close()
	if _, ok := r.Get("b"); ok {
		t.Fatal("corrupt frame applied")
	}
	if _, ok := r.Get("a"); !ok {
		t.Fatal("good frame lost")
	}
	if r.Stats().TruncatedBytes == 0 {
		t.Fatal("truncation not reported")
	}
}

func TestCorruptSnapshotRefusesToOpen(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, Options{})
	must(t, s.Put("a", []byte("1")))
	must(t, s.Compact())
	must(t, s.Close())
	p := filepath.Join(dir, snapName(1))
	data, _ := os.ReadFile(p)
	must(t, os.WriteFile(p, data[:len(data)-3], 0o644))
	if _, err := Open(dir, Options{}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

// A crash at each compaction step must leave a directory that recovers every
// committed write, and later writes must survive another restart.
func TestCrashDuringCompaction(t *testing.T) {
	for _, step := range []string{"snapshot-start", "snapshot-written", "snapshot-renamed", "wal-created"} {
		t.Run(step, func(t *testing.T) {
			dir := t.TempDir()
			crash := errors.New("crash")
			armed := false
			s := open(t, dir, Options{hook: func(st string) error {
				if armed && st == step {
					return crash
				}
				return nil
			}})
			for i := 0; i < 5; i++ {
				must(t, s.Put("k"+strconv.Itoa(i), []byte("v")))
			}
			must(t, s.Compact()) // a clean generation first
			must(t, s.Put("late", []byte("1")))
			armed = true
			if err := s.Compact(); !errors.Is(err, crash) {
				t.Fatalf("compact err = %v", err)
			}
			s.abandon()

			r := open(t, dir, Options{})
			for _, k := range []string{"k0", "k4", "late"} {
				if _, ok := r.Get(k); !ok {
					t.Fatalf("%s lost after crash at %s", k, step)
				}
			}
			must(t, r.Put("post", []byte("2")))
			must(t, r.Close())
			r = open(t, dir, Options{})
			defer r.Close()
			if _, ok := r.Get("post"); !ok {
				t.Fatal("post-recovery write lost")
			}
			ents, _ := os.ReadDir(dir)
			for _, e := range ents {
				if strings.HasSuffix(e.Name(), ".tmp") {
					t.Fatalf("leftover %s", e.Name())
				}
			}
		})
	}
}

// abandon drops the store without flushing or compacting, as a killed process
// would.
func (s *Store) abandon() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.wal != nil {
		s.wal.Close()
	}
	s.releaseLock()
}

func TestSecondOpenLocked(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, Options{})
	if _, err := Open(dir, Options{}); !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v, want ErrLocked", err)
	}
	// With a timeout, Open waits for the holder to go away.
	go func() { time.Sleep(150 * time.Millisecond); s.Close() }()
	r, err := Open(dir, Options{LockTimeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("waiting open: %v", err)
	}
	r.Close()
}

func TestClosedStore(t *testing.T) {
	s := open(t, t.TempDir(), Options{})
	s.Close()
	if err := s.Put("a", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestConcurrentWritersRace(t *testing.T) {
	s := open(t, t.TempDir(), Options{CompactAt: 4 << 10, Sync: SyncNever})
	defer s.Close()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				k := fmt.Sprintf("g%d/%d", g, i%10)
				if err := s.Put(k, []byte("v")); err != nil {
					t.Error(err)
					return
				}
				_, _ = s.Append("log", []byte(k))
				s.Get(k)
				s.Keys("g")
				s.LogRead("log", 0, 5)
			}
		}()
	}
	wg.Wait()
	if n := len(s.Keys("g")); n != 80 {
		t.Fatalf("keys = %d", n)
	}
}

// TestKilledWriter starts a child process that commits numbered records, kills
// it with SIGKILL mid-stream, and checks that every record the child reported
// as durable survived and nothing is half-applied.
func TestKilledWriter(t *testing.T) {
	if os.Getenv("STORE_CHILD_DIR") != "" {
		return
	}
	if testing.Short() {
		t.Skip("spawns a process")
	}
	dir := t.TempDir()
	for round := 0; round < 3; round++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestChildWriter$")
		cmd.Env = append(os.Environ(), "STORE_CHILD_DIR="+dir)
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		must(t, cmd.Start())
		acked := 0
		buf := make([]byte, 64)
		var pending string
		deadline := time.Now().Add(10 * time.Second)
		for acked < 300+round*50 && time.Now().Before(deadline) {
			n, err := out.Read(buf)
			if err != nil {
				break
			}
			pending += string(buf[:n])
			for {
				i := strings.IndexByte(pending, '\n')
				if i < 0 {
					break
				}
				if v, err := strconv.Atoi(pending[:i]); err == nil {
					acked = v
				}
				pending = pending[i+1:]
			}
		}
		_ = cmd.Process.Signal(syscall.SIGKILL)
		_ = cmd.Wait()
		if acked == 0 {
			t.Fatal("child never acknowledged a write")
		}
		r := open(t, dir, Options{})
		v, ok := r.Get("counter")
		if !ok {
			t.Fatal("counter missing")
		}
		got, _ := strconv.Atoi(string(v))
		if got < acked {
			t.Fatalf("round %d: counter %d < acknowledged %d: lost a durable write", round, got, acked)
		}
		// The batch writes counter and a log entry together: they must agree.
		_, next := r.LogRange("log")
		if int(next)-1 != got {
			t.Fatalf("round %d: counter %d but log has %d entries: torn batch", round, got, next-1)
		}
		r.Close()
	}
}

// TestChildWriter is the body of the killed process; it does nothing unless
// STORE_CHILD_DIR is set.
func TestChildWriter(t *testing.T) {
	dir := os.Getenv("STORE_CHILD_DIR")
	if dir == "" {
		t.Skip("child helper")
	}
	s, err := Open(dir, Options{CompactAt: 8 << 10, MaxLogEntries: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	if v, ok := s.Get("counter"); ok {
		n, _ = strconv.Atoi(string(v))
	}
	for {
		n++
		b := (&Batch{}).Put("counter", []byte(strconv.Itoa(n))).Append("log", []byte(strconv.Itoa(n)))
		if err := s.Commit(b); err != nil {
			t.Fatal(err)
		}
		fmt.Println(n)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestDropLog(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, Options{})
	s.Append("l", []byte("a"))
	before := s.Stats().DataBytes
	must(t, s.Commit((&Batch{}).DropLog("l")))
	if s.Stats().DataBytes >= before || s.Stats().Logs != 0 {
		t.Fatalf("stats after drop: %+v", s.Stats())
	}
	must(t, s.Close())
	s = open(t, dir, Options{})
	defer s.Close()
	if s.Stats().Logs != 0 {
		t.Fatal("drop not replayed")
	}
}
