package advance

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/room/store"
)

// A handler that panics is recorded on its job and backs off like a failing
// one, and the jobs after it in the same scan still advance. Before the fix
// the panic escaped RunDue: every scan stopped at the first job ("a" sorts
// before "b"), so one bad job starved the others.
func TestPanickingHandlerBacksOffAndOthersAdvance(t *testing.T) {
	for _, where := range []string{"ready", "advance", "apply"} {
		t.Run(where, func(t *testing.T) {
			clk := &clock{t: epoch}
			e := newEnv(t, t.TempDir(), clk)
			defer e.st.Close()
			var logMu sync.Mutex
			var logs strings.Builder
			e.r.opts.Logf = func(format string, args ...any) {
				logMu.Lock()
				defer logMu.Unlock()
				logs.WriteString(format)
			}
			for _, g := range []string{"a", "b"} {
				if _, err := e.reg.Create(g, g, nil); err != nil {
					t.Fatal(err)
				}
			}
			bad := Handler{
				Ready: func(context.Context, View) (bool, error) {
					if where == "ready" {
						panic("ready bug")
					}
					return true, nil
				},
				Advance: func(context.Context, Advance) (Result, error) {
					if where == "advance" {
						panic("advance bug")
					}
					return Result{Apply: func(*store.Batch) { panic("apply bug") }}, nil
				},
			}
			var good atomic.Int32
			must(t, e.r.Handle("bad", bad))
			must(t, e.r.Handle("good", counterHandler(new(atomic.Bool), &good, e.st)))
			_, err := e.r.Schedule(Job{Group: "a", Name: "j", Kind: "bad", Interval: time.Hour, Deadline: epoch.Add(time.Hour)})
			must(t, err)
			_, err = e.r.Schedule(Job{Group: "b", Name: "j", Kind: "good", Interval: time.Hour, Deadline: epoch.Add(time.Minute)})
			must(t, err)
			clk.Advance(2 * time.Minute) // b is due; a is before its deadline, so Ready decides

			n, err := e.r.RunDue(context.Background())
			if err != nil || n != 1 || good.Load() != 1 {
				t.Fatalf("RunDue = %d, %v; good handler calls %d; want the good job advanced", n, err, good.Load())
			}
			j, err := e.r.Job("a", "j")
			must(t, err)
			if where == "ready" {
				// A Ready failure is logged and the job waits for its deadline.
				if j.Round != 0 || j.Pending != nil {
					t.Fatalf("job after Ready panic %+v", j)
				}
			} else if j.Round != 0 || j.Attempts != 1 || !strings.Contains(j.LastError, "panicked") || !j.RetryAt.After(clk.Now()) {
				t.Fatalf("job after %s panic %+v, want one failed attempt with backoff", where, j)
			}
			logMu.Lock()
			defer logMu.Unlock()
			if !strings.Contains(logs.String(), "panicked") {
				t.Fatalf("panic not logged: %q", logs.String())
			}
		})
	}
}
