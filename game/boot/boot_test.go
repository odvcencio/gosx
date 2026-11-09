package boot

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type memMarker struct {
	mu      sync.Mutex
	m       map[string]string
	failSet bool
}

func newMarker() *memMarker { return &memMarker{m: map[string]string{}} }
func (k *memMarker) Get(key string) (string, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	return v, ok
}
func (k *memMarker) Set(key, v string) error {
	if k.failSet {
		return errors.New("no storage")
	}
	k.m[key] = v
	return nil
}
func (k *memMarker) Delete(key string) error { delete(k.m, key); return nil }

const sec = time.Second

func TestProgressWeightedAndMonotonic(t *testing.T) {
	g := New(Config{}, Task{Name: "assets", Weight: 3}, Task{Name: "models", Weight: 1})
	g.Set("assets", 1, 2)
	if p := g.Progress(); p != 0.375 {
		t.Fatalf("progress %v", p)
	}
	g.Set("assets", 2, 2)
	g.Update(sec)
	if g.Phase() != PhaseLoading || g.Progress() != 0.75 {
		t.Fatal(g.Phase(), g.Progress())
	}
	g.Add(Task{Name: "pose", Weight: 4}) // would drop the average to 0.375
	if g.Progress() != 0.75 {
		t.Fatalf("progress went down: %v", g.Progress())
	}
	g.Complete("models")
	g.Complete("pose")
	g.Update(sec)
	if !g.Ready() || g.Progress() != 1 {
		t.Fatal(g.Phase(), g.Progress())
	}
}

func TestPolledTasks(t *testing.T) {
	done := 0
	g := New(Config{}, CountTask("models", 1, func() (int, int) { return done, 4 }))
	g.Update(sec)
	done = 4
	g.Update(sec)
	if !g.Ready() {
		t.Fatal(g.Snapshot())
	}
}

func TestStallRecoveryOrder(t *testing.T) {
	mk := newMarker()
	reloads, retries := 0, 0
	g := New(Config{
		StallTimeout: 10 * sec, Retries: 1,
		Reload: func() { reloads++ }, Marker: mk, MarkerKey: "k",
	}, Task{Name: "a", Retry: func() { retries++ }})
	g.Set("a", 1, 10)
	for i := 0; i < 9; i++ {
		g.Update(sec)
	}
	if retries != 0 || g.Phase() != PhaseLoading {
		t.Fatal("too early")
	}
	g.Update(sec) // 10 s without progress: soft retry
	if retries != 1 || g.Phase() != PhaseLoading || g.Snapshot().SoftRetries != 1 {
		t.Fatalf("soft retry: %d %v", retries, g.Phase())
	}
	for i := 0; i < 9; i++ {
		g.Update(sec)
	}
	g.Update(sec) // second stall: reload step
	if g.Phase() != PhaseRecovering || reloads != 0 {
		t.Fatalf("expected recovering, reloads=%d phase=%v", reloads, g.Phase())
	}
	if v, _ := mk.Get("k"); v != "retried" {
		t.Fatal("marker not written")
	}
	g.Update(100 * time.Millisecond)
	if reloads != 0 {
		t.Fatal("reload before delay")
	}
	g.Update(200 * time.Millisecond)
	g.Update(sec)
	if reloads != 1 {
		t.Fatalf("reloads = %d", reloads)
	}
	g.Update(sec)
	if reloads != 1 {
		t.Fatal("reload fired twice")
	}

	// New page load after the reload: marker present, so the next stall fails.
	reloads = 0
	g2 := New(Config{StallTimeout: 10 * sec, Retries: -1, Reload: func() { reloads++ }, Marker: mk, MarkerKey: "k"}, Task{Name: "a"})
	if g2.Snapshot().Attempt != 1 {
		t.Fatal("attempt")
	}
	g2.Update(10 * sec)
	if g2.Phase() != PhaseFailed || reloads != 0 {
		t.Fatalf("want failed without reload: %v %d", g2.Phase(), reloads)
	}
	// Late completion still reaches ready and clears the marker.
	g2.Complete("a")
	g2.Update(sec)
	if !g2.Ready() {
		t.Fatal("late ready")
	}
	if _, ok := mk.Get("k"); ok {
		t.Fatal("marker should clear on ready")
	}
}

func TestNoReloadWhenStorageUnavailable(t *testing.T) {
	mk := newMarker()
	mk.failSet = true
	reloads := 0
	g := New(Config{StallTimeout: sec, Retries: -1, Reload: func() { reloads++ }, Marker: mk, MarkerKey: "k"}, Task{Name: "a"})
	g.Update(2 * sec)
	g.Update(sec)
	if g.Phase() != PhaseFailed || reloads != 0 {
		t.Fatalf("%v %d", g.Phase(), reloads)
	}
	// Without any marker configured the gate never reloads.
	g = New(Config{StallTimeout: sec, Retries: -1, Reload: func() { reloads++ }}, Task{Name: "a"})
	g.Update(2 * sec)
	if g.Phase() != PhaseFailed || reloads != 0 {
		t.Fatal(g.Phase(), reloads)
	}
}

func TestHiddenPausesStallTimer(t *testing.T) {
	g := New(Config{StallTimeout: 10 * sec, Retries: -1}, Task{Name: "a"})
	g.Update(5 * sec)
	g.SetHidden(true)
	g.Update(60 * sec)
	if g.Phase() != PhaseLoading {
		t.Fatal("hidden time counted")
	}
	g.SetHidden(false)
	g.Update(9 * sec)
	if g.Phase() != PhaseLoading {
		t.Fatal("timer should restart on show")
	}
	g.Update(sec)
	if g.Phase() != PhaseFailed {
		t.Fatal(g.Phase())
	}
}

func TestProgressResetsStallTimer(t *testing.T) {
	g := New(Config{StallTimeout: 10 * sec, Retries: -1}, Task{Name: "a"})
	for i := 1; i <= 30; i++ {
		g.Set("a", float64(i), 100)
		g.Update(5 * sec)
	}
	if g.Phase() != PhaseLoading {
		t.Fatal("steady progress must not stall")
	}
}

func TestFailTriggersRecoveryAndRetryClearsError(t *testing.T) {
	retried := 0
	g := New(Config{StallTimeout: time.Hour, Retries: 1}, Task{Name: "a", Retry: func() { retried++ }})
	g.Fail("a", errors.New("404"))
	if g.Snapshot().Err == nil {
		t.Fatal("err not visible")
	}
	g.Update(sec)
	if retried != 1 || g.Snapshot().Err != nil {
		t.Fatal(retried, g.Snapshot().Err)
	}
	g.Fail("a", errors.New("404 again"))
	g.Update(sec)
	if g.Phase() != PhaseFailed || g.Snapshot().Err == nil {
		t.Fatal(g.Phase())
	}
}

func TestManualRetry(t *testing.T) {
	mk := newMarker()
	_ = mk.Set("k", "retried")
	reloads := 0
	g := New(Config{Reload: func() { reloads++ }, Marker: mk, MarkerKey: "k"}, Task{Name: "a"})
	g.Retry()
	if reloads != 1 {
		t.Fatal("reload")
	}
	if _, ok := mk.Get("k"); ok {
		t.Fatal("manual retry must give the next load a fresh budget")
	}
}

func TestOnChangeOnlyOnChange(t *testing.T) {
	var snaps []Snapshot
	g := New(Config{OnChange: func(s Snapshot) { snaps = append(snaps, s) }}, Task{Name: "a"})
	g.Set("a", 1, 4)
	g.Set("a", 1, 4)
	g.Update(sec)
	g.Update(sec)
	n := len(snaps)
	g.Set("a", 4, 4)
	g.Update(sec)
	if len(snaps) <= n || snaps[len(snaps)-1].Phase != PhaseReady {
		t.Fatalf("%d snaps, last %+v", len(snaps), snaps[len(snaps)-1])
	}
	for i := 1; i < len(snaps); i++ {
		if snaps[i].Phase == snaps[i-1].Phase && snaps[i].Progress == snaps[i-1].Progress && len(snaps[i].Tasks) == len(snaps[i-1].Tasks) &&
			snaps[i].Tasks[0].Done == snaps[i-1].Tasks[0].Done {
			t.Fatalf("duplicate emit at %d", i)
		}
	}
}

func TestConcurrentReports(t *testing.T) {
	g := New(Config{}, Task{Name: "a"}, Task{Name: "b"})
	var wg sync.WaitGroup
	for _, n := range []string{"a", "b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 1; i <= 100; i++ {
				g.Set(n, float64(i), 100)
			}
		}()
	}
	wg.Wait()
	g.Update(sec)
	if !g.Ready() {
		t.Fatal(g.Snapshot())
	}
}
