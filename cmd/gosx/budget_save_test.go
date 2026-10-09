package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/perf/budget"
)

func TestBudgetSaveRejectsEditAfterStaging(t *testing.T) {
	for _, change := range []string{"contents", "replacement", "mtime"} {
		t.Run(change, func(t *testing.T) {
			dir, path := budgetCommandFixture(t)
			inputs, err := budget.LoadDerivationInputs(path, budget.LoadOptions{RootDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			old, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			r, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			relative, err := filepath.Rel(dir, path)
			if err != nil {
				t.Fatal(err)
			}
			temp, err := stageBudgetAt(r, relative, []byte("obsolete proposal"))
			if err != nil {
				t.Fatal(err)
			}
			want := old
			switch change {
			case "contents":
				want = bytes.Clone(old)
				position := bytes.Index(want, []byte(`"appReserveBytes":`))
				if position < 0 {
					t.Fatal("reserve field missing")
				}
				position += len(`"appReserveBytes":`)
				for want[position] == ' ' || want[position] == '\t' {
					position++
				}
				if want[position] == '1' {
					want[position] = '2'
				} else {
					want[position] = '1'
				}
				if err := os.WriteFile(path, want, info.Mode().Perm()); err != nil {
					t.Fatal(err)
				}
				// A content edit must be detected even if mtime is restored.
				if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				next := filepath.Join(filepath.Dir(path), "replacement.json")
				if err := os.WriteFile(next, old, info.Mode().Perm()); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(next, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(next, path); err != nil {
					t.Fatal(err)
				}
			case "mtime":
				changed := info.ModTime().Add(2 * time.Second)
				if err := os.Chtimes(path, changed, changed); err != nil {
					t.Fatal(err)
				}
			}
			if err := replaceBudgetAt(r, relative, temp, inputs); err == nil || err.Error() != "budget file changed since it was read; re-run derive" {
				t.Error("stale save did not report a concurrent edit", err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, want) {
				t.Error("stale save replaced newer bytes", err)
			}
			if _, err := r.Stat(temp); !os.IsNotExist(err) {
				t.Error("stale proposal left scratch", err)
			}
		})
	}
}

func TestBudgetSaveCLIRejectsEditWhileWaitingForLock(t *testing.T) {
	for _, mode := range []string{"write", "out"} {
		t.Run(mode, func(t *testing.T) {
			dir, path := budgetCommandFixture(t)
			old, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			r, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			relative, err := filepath.Rel(dir, path)
			if err != nil {
				t.Fatal(err)
			}
			lock, err := openBudgetLock(r, relative+".lock")
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if err := acquireBudgetLock(lock); err != nil {
				t.Fatal(err)
			}
			args := []string{"derive", "--budget", path, "--root", dir, "--write"}
			if mode == "out" {
				args = append(args[:len(args)-1], "--out", path)
			}
			type result struct {
				code               int
				output, diagnostic string
			}
			done := make(chan result, 1)
			go func() {
				code, output, diagnostic := runBudgetTest(args...)
				done <- result{code, output, diagnostic}
			}()
			// The lock prevents replacement while the proposal is staged. Seeing
			// its temp file proves derive has already read the original budget.
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(5 * time.Millisecond)
			defer tick.Stop()
			staged := false
			for !staged {
				select {
				case early := <-done:
					t.Fatalf("save bypassed the exclusive lock: %v", early)
				case <-deadline.C:
					t.Fatal("proposal was not staged before timeout")
				case <-tick.C:
					matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".gosx-budget-*"))
					if err != nil {
						t.Fatal(err)
					}
					staged = len(matches) > 0
				}
			}
			select {
			case early := <-done:
				t.Fatalf("staged save bypassed the exclusive lock: %v", early)
			case <-time.After(150 * time.Millisecond):
			}
			newer := append(bytes.Clone(old), '\n')
			if err := os.WriteFile(path, newer, 0600); err != nil {
				t.Fatal(err)
			}
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case saved := <-done:
				if saved.code != 2 || saved.output != "" || saved.diagnostic != "budget file changed since it was read; re-run derive\n" {
					t.Fatal("concurrent edit did not yield an actionable input error", saved)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("save did not finish after releasing the lock")
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, newer) {
				t.Fatal("CLI discarded concurrent edit", err)
			}
			matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".gosx-budget-*"))
			if len(matches) != 0 {
				t.Fatal("failed CLI save left scratch")
			}
		})
	}
}

func TestBudgetSaveRejectsNonregularLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.json")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".lock", 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeBudgetAtomically(path, []byte("replacement")); err == nil {
		t.Fatal("directory accepted as writer lock")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "original" {
		t.Fatal("invalid lock changed output", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".gosx-budget-*"))
	if len(matches) != 0 {
		t.Fatal("invalid lock left scratch")
	}
	var diagnostic bytes.Buffer
	if code := budgetDiagnostic(&diagnostic, errBudgetChanged, 2, "environment", "output", ""); code != 2 || !strings.Contains(diagnostic.String(), "re-run derive") {
		t.Fatal("changed budget diagnostic is not actionable")
	}
}

func TestBudgetSaveConcurrentProposalsHaveOneWinner(t *testing.T) {
	dir, path := budgetCommandFixture(t)
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	relative, err := filepath.Rel(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	inputs := [2]*budget.Inputs{}
	temps := [2]string{}
	proposals := [2][]byte{[]byte("first proposal"), []byte("second proposal")}
	for i := range inputs {
		inputs[i], err = budget.LoadDerivationInputs(path, budget.LoadOptions{RootDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		temps[i], err = stageBudgetAt(r, relative, proposals[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := [2]error{}
	var writers sync.WaitGroup
	for i := range inputs {
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-start
			results[i] = replaceBudgetAt(r, relative, temps[i], inputs[i])
		}()
	}
	close(start)
	writers.Wait()
	winners := 0
	var want []byte
	for i, err := range results {
		if err == nil {
			winners++
			want = proposals[i]
		} else if err.Error() != "budget file changed since it was read; re-run derive" {
			t.Error("unexpected save error", err)
		}
		if _, err := r.Stat(temps[i]); !os.IsNotExist(err) {
			t.Error("concurrent save left scratch", err)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent proposals had %d winners", winners)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("saved bytes differ from the winning proposal", err)
	}
}
