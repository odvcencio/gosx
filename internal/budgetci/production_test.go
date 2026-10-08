//go:build linux

package budgetci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/perf/budget"
)

func helperScript(executable string) []byte {
	quoted := "'" + strings.ReplaceAll(executable, "'", "'\\''") + "'"
	return []byte("#!/bin/sh\nexec " + quoted + " '-test.run=^TestProductionHelper$' -- \"$@\"\n")
}

// The synthetic compiler runs real subprocesses and a real local HTTP server.
// It verifies command ownership without repeating expensive production builds.
func TestProductionHelper(t *testing.T) {
	if os.Getenv("GOSX_TEST_PRODUCTION_HELPER") != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	write := func(path string, data []byte, mode os.FileMode) {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if len(args) == 0 || args[0] == "linger" {
		if len(args) != 0 {
			write(os.Getenv("GOSX_TEST_CHILD_PID"), []byte(strconv.Itoa(os.Getpid())), 0600)
		}
		if len(args) == 0 {
			listener, err := net.Listen("tcp4", os.Getenv("GOSX_LISTEN_ADDR"))
			if err != nil {
				t.Fatal(err)
			}
			http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/readyz" || r.URL.Path == "/api/health" {
					if os.Getenv("GOSX_TEST_UNREADY") == "1" {
						w.WriteHeader(http.StatusServiceUnavailable)
					}
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"gowork": os.Getenv("GOWORK"), "root": os.Getenv("GOSX_APP_ROOT"), "url": os.Getenv("PUBLIC_URL"), "secretReady": len(os.Getenv("SESSION_SECRET")) == 64})
			}))
		}
		select {}
	}
	if args[0] == "child-build" {
		child := exec.Command(executable, "-test.run=^TestProductionHelper$", "--", "linger")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		if args[len(args)-1] != "abandon" {
			child.Wait()
		} else {
			for i := 0; i < 500; i++ {
				if _, err := os.Stat(os.Getenv("GOSX_TEST_CHILD_PID")); err == nil {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("abandoned child did not start")
		}
		return
	}
	if len(args) == 4 && args[0] == "build" && args[1] == "-o" && args[3] == "./cmd/gosx" {
		write(args[2], helperScript(executable), 0755)
		write(filepath.Join(filepath.Dir(args[2]), "environment.json"), []byte(fmt.Sprintf(`{"gowork":%q,"cache":%q,"temp":%q}`, os.Getenv("GOWORK"), os.Getenv("GOCACHE"), os.Getenv("TMPDIR"))), 0600)
		return
	}
	if len(args) == 4 && args[0] == "init" && args[2] == "--module" && args[3] == "example.com/budget-fixture" {
		write(filepath.Join(args[1], "app/page.gsx"), []byte("package app\n"), 0600)
		return
	}
	if len(args) == 5 && args[0] == "build" && args[1] == "--prod" && args[2] == "--perf-app" {
		manifest := buildmanifest.Manifest{PerfAssetUses: &buildmanifest.PerfAssetUses{Version: 1, Assets: []buildmanifest.PerfAssetUse{{ID: "framework/synthetic.js", SHA256: strings.Repeat("a", 64), URL: "/synthetic.js", Owner: "framework", Kind: "js", Phase: "startup", Condition: "always", Dependencies: []string{}}}}}
		if os.Getenv("GOSX_TEST_BAD_MANIFEST") == "1" {
			manifest.PerfAssetUses = nil
		}
		data, _ := json.Marshal(manifest)
		dist := filepath.Join(args[4], "dist")
		write(filepath.Join(dist, "build.json"), data, 0600)
		write(filepath.Join(dist, "server/app"), helperScript(executable), 0755)
		return
	}
	t.Fatal("unsupported synthetic compiler command")
}

func productionFixture(t *testing.T) (*Compiler, string) {
	t.Helper()
	opts, _ := approvalFixture(t)
	for _, name := range []string{"page.gsx", "page.server.go"} {
		path := filepath.Join(opts.Root, "perf/wire/testdata/counter", name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package counter\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	gitFixture(t, opts.Root, "add", ".")
	gitFixture(t, opts.Root, "commit", "-qm", "Add synthetic source")
	sha := gitFixture(t, opts.Root, "rev-parse", "HEAD")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "go"), helperScript(executable), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSX_TEST_PRODUCTION_HELPER", "1")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	c, err := BuildCompiler(context.Background(), opts.Root, sha, t.TempDir())
	if err != nil {
		t.Fatal("synthetic compiler failed", err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c, sha
}

func TestProductionCompilerBindsCleanRevisionAndPreservesEnvironment(t *testing.T) {
	c, sha := productionFixture(t)
	data, err := os.ReadFile(filepath.Join(c.owned, "environment.json"))
	var got map[string]string
	if err != nil || json.Unmarshal(data, &got) != nil || got["gowork"] != "off" || got["cache"] != os.Getenv("GOCACHE") || got["temp"] != os.Getenv("TMPDIR") {
		t.Fatal("provided build environment changed", err)
	}
	if _, err := BuildCompiler(context.Background(), c.SourceRoot, strings.Repeat("f", 40), t.TempDir()); err == nil {
		t.Fatal("different source revision accepted")
	}
	if err := os.WriteFile(filepath.Join(c.SourceRoot, "budget.json"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.BuildApplication(context.Background(), "scaffold", t.TempDir()); err == nil {
		t.Fatal("dirty measured source accepted")
	}
	if c.SourceSHA != sha {
		t.Fatal("source binding mutated")
	}
	if data, _ := json.Marshal(c); string(data) != "{}" {
		t.Fatal("private build metadata became public")
	}
}

func TestProductionApplicationsVerifyMetadataAndOwnOnlyOutputs(t *testing.T) {
	for _, app := range []string{"docs", "scaffold"} {
		t.Run(app, func(t *testing.T) {
			c, sha := productionFixture(t)
			a, err := c.BuildApplication(context.Background(), app, t.TempDir())
			if err != nil || a.SourceSHA != sha || a.Manifest.PerfAssetUses == nil {
				t.Fatal("production metadata missing", err)
			}
			if app == "scaffold" {
				for _, name := range []string{"page.gsx", "page.server.go"} {
					if _, err := os.Stat(filepath.Join(a.Root, "app/counter", name)); err != nil {
						t.Fatal("interactive fixture missing", err)
					}
				}
				data, _ := os.ReadFile(filepath.Join(a.Root, "app/route.config.json"))
				if !strings.Contains(string(data), `"prerender":true`) {
					t.Fatal("static scaffold fixture missing")
				}
			}
			if data, _ := json.Marshal(a); string(data) != "{}" {
				t.Fatal("application paths became public")
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(a.owned); !os.IsNotExist(err) {
				t.Fatal("owned output retained")
			}
			if _, err := os.Stat(filepath.Join(c.SourceRoot, "budget.json")); err != nil {
				t.Fatal("cleanup removed source")
			}
		})
	}
	c, _ := productionFixture(t)
	dist := filepath.Join(c.SourceRoot, "examples/gosx-docs/dist")
	if err := os.MkdirAll(dist, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := c.BuildApplication(context.Background(), "docs", t.TempDir()); err == nil {
		t.Fatal("pre-existing output accepted")
	}
	if _, err := os.Stat(dist); err != nil {
		t.Fatal("pre-existing output removed")
	}
	t.Setenv("GOSX_TEST_BAD_MANIFEST", "1")
	if _, err := c.BuildApplication(context.Background(), "scaffold", t.TempDir()); err == nil {
		t.Fatal("legacy manifest accepted as measured evidence")
	}
}

func TestProductionServerReadinessIsolationAndCleanup(t *testing.T) {
	for _, app := range []string{"docs", "scaffold"} {
		t.Run(app, func(t *testing.T) {
			c, _ := productionFixture(t)
			a, err := c.BuildApplication(context.Background(), app, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, err := a.Serve(ctx, filepath.Join(t.TempDir(), "server.log"))
			if err != nil {
				t.Fatal("local server did not become ready", err)
			}
			defer s.Close()
			response, err := s.Client.Get(s.BaseURL + "/environment")
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(response.Body)
			response.Body.Close()
			var got struct {
				Gowork, Root, URL string
				SecretReady       bool
			}
			if json.Unmarshal(data, &got) != nil || got.Gowork != "off" || got.Root != a.DistDir || got.URL != s.BaseURL || !got.SecretReady || !strings.HasPrefix(s.BaseURL, "http://127.0.0.1:") {
				t.Fatal("server isolation differs")
			}
			if data, _ := json.Marshal(s); string(data) != "{}" {
				t.Fatal("server bindings became public")
			}
			cancel()
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if s.cmd.ProcessState == nil {
				t.Fatal("owned server was not waited")
			}
			if _, err := s.Client.Get(s.BaseURL); err == nil {
				t.Fatal("server still reachable after cleanup")
			}
		})
	}
}

func TestProductionCancellationStopsOwnedChildOnly(t *testing.T) {
	productionFixture(t)
	executable, _ := os.Executable()
	childPID := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("GOSX_TEST_CHILD_PID", childPID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- productionCommand(ctx, t.TempDir(), filepath.Join(t.TempDir(), "build.log"), executable, "-test.run=^TestProductionHelper$", "--", "child-build")
	}()
	deadline := time.Now().Add(10 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(childPID); err == nil {
			pid, _ = strconv.Atoi(string(data))
			if pid > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("owned child did not start")
	}
	cancel()
	select {
	case err := <-done:
		var typed *budget.InputError
		if !errors.As(err, &typed) || typed.Code != "environment" || typed.Pointer != "/production/build" {
			t.Fatal("cancelled build did not return fixed error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled build did not close")
	}
	if fields := processFields(pid); fields != nil && fields[0] != "Z" {
		t.Fatal("owned child outlived cancelled build")
	}
	if os.Getpid() <= 0 || processFields(os.Getpid()) == nil {
		t.Fatal("unrelated process was stopped")
	}
}

func TestProductionClosesAbandonedToolsAndReadinessFailure(t *testing.T) {
	c, _ := productionFixture(t)
	executable, _ := os.Executable()
	childPID := filepath.Join(t.TempDir(), "abandoned.pid")
	t.Setenv("GOSX_TEST_CHILD_PID", childPID)
	if err := productionCommand(context.Background(), t.TempDir(), filepath.Join(t.TempDir(), "build.log"), executable, "-test.run=^TestProductionHelper$", "--", "child-build", "abandon"); err != nil {
		t.Fatal("successful parent failed cleanup", err)
	}
	data, err := os.ReadFile(childPID)
	pid, _ := strconv.Atoi(string(data))
	if err != nil || pid <= 0 {
		t.Fatal("abandoned tool did not start", err)
	}
	if fields := processFields(pid); fields != nil && fields[0] != "Z" {
		t.Fatal("tool outlived successful parent")
	}
	a, err := c.BuildApplication(context.Background(), "docs", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	t.Setenv("GOSX_TEST_UNREADY", "1")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s, err := a.Serve(ctx, filepath.Join(t.TempDir(), "unready.log"))
	var typed *budget.InputError
	if s != nil || !errors.As(err, &typed) || typed.Pointer != "/production/readiness" || typed.Code != "timeout" && typed.Code != "environment" {
		t.Fatal("unready server became measurement evidence", err)
	}
}
