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
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/perf/budget"
)

func helperScript(executable, config string) []byte {
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	return []byte("#!/bin/sh\nexec " + quote(executable) + " '-test.run=^TestProductionHelper$' -- " + quote(config) + " \"$@\"\n")
}

type productionHelperConfig struct {
	Go, ChildPID         string
	BadManifest, Unready bool
}

func configureProductionHelper(t *testing.T, c *Compiler, update func(*productionHelperConfig)) string {
	t.Helper()
	path := filepath.Join(c.SourceRoot, ".git/production-helper.json")
	data, err := os.ReadFile(path)
	var config productionHelperConfig
	if err != nil || json.Unmarshal(data, &config) != nil {
		t.Fatal("helper configuration missing", err)
	}
	update(&config)
	data, err = json.Marshal(config)
	if err != nil || os.WriteFile(path, data, 0600) != nil {
		t.Fatal("helper configuration failed", err)
	}
	return path
}

// The synthetic compiler runs real subprocesses and a real local HTTP server.
// It verifies command ownership without repeating expensive production builds.
func TestProductionHelper(t *testing.T) {
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		return
	}
	configPath := args[0]
	data, err := os.ReadFile(configPath)
	var config productionHelperConfig
	if err != nil || json.Unmarshal(data, &config) != nil {
		t.Fatal("helper configuration missing", err)
	}
	args = args[1:]
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
			write(config.ChildPID, []byte(strconv.Itoa(os.Getpid())), 0600)
		}
		if len(args) == 0 {
			listener, err := net.Listen("tcp4", os.Getenv("GOSX_LISTEN_ADDR"))
			if err != nil {
				t.Fatal(err)
			}
			http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/readyz" || r.URL.Path == "/api/health" {
					// Match the docs readiness gate: development bypasses asset checks.
					if os.Getenv("GOSX_DEV") == "" && config.Unready {
						w.WriteHeader(http.StatusServiceUnavailable)
					}
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"gowork": os.Getenv("GOWORK"), "root": os.Getenv("GOSX_APP_ROOT"), "url": os.Getenv("PUBLIC_URL"), "secretReady": len(os.Getenv("SESSION_SECRET")) == 64, "mode": os.Getenv("GOSX_ENV"), "dev": os.Getenv("GOSX_DEV"), "cache": os.Getenv("GOCACHE"), "temp": os.Getenv("TMPDIR"), "goTemp": os.Getenv("GOTMPDIR")})
			}))
		}
		select {}
	}
	if args[0] == "child-build" {
		child := exec.Command(executable, "-test.run=^TestProductionHelper$", "--", configPath, "linger")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		if args[len(args)-1] != "abandon" {
			child.Wait()
		} else {
			for i := 0; i < 500; i++ {
				if _, err := os.Stat(config.ChildPID); err == nil {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("abandoned child did not start")
		}
		return
	}
	if len(args) == 4 && args[0] == "build" && args[1] == "-o" && args[3] == "./cmd/gosx" {
		write(args[2], helperScript(executable, configPath), 0755)
		write(filepath.Join(filepath.Dir(args[2]), "environment.json"), []byte(fmt.Sprintf(`{"gowork":%q,"cache":%q,"temp":%q}`, os.Getenv("GOWORK"), os.Getenv("GOCACHE"), os.Getenv("TMPDIR"))), 0600)
		return
	}
	if len(args) == 3 && args[0] == "mod" && args[1] == "edit" && args[2] == "-json" {
		cmd := exec.Command(config.Go, args...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatal("module inspection failed", err)
		}
		os.Exit(0)
	}
	if len(args) == 4 && args[0] == "init" && args[2] == "--module" && args[3] == "example.com/budget-fixture" {
		write(filepath.Join(args[1], "app/page.gsx"), []byte("package app\n"), 0600)
		// Match the scaffold's replacement back to the verified CLI source.
		source := strconv.Quote(filepath.ToSlash(filepath.Dir(filepath.Dir(configPath))))
		write(filepath.Join(args[1], "go.mod"), []byte("module example.com/budget-fixture\n\ngo 1.26\n\nreplace m31labs.dev/gosx => "+source+"\n"), 0600)
		return
	}
	if len(args) == 5 && args[0] == "build" && args[1] == "--prod" && args[2] == "--perf-app" {
		manifest := buildmanifest.Manifest{PerfAssetUses: &buildmanifest.PerfAssetUses{Version: 1, Assets: []buildmanifest.PerfAssetUse{{ID: "framework/synthetic.js", SHA256: strings.Repeat("a", 64), URL: "/synthetic.js", Owner: "framework", Kind: "js", Phase: "startup", Condition: "always", Dependencies: []string{}}}}}
		if config.BadManifest {
			manifest.PerfAssetUses = nil
		}
		data, _ := json.Marshal(manifest)
		dist := filepath.Join(args[4], "dist")
		write(filepath.Join(dist, "build.json"), data, 0600)
		write(filepath.Join(dist, "server/app"), helperScript(executable, configPath), 0755)
		return
	}
	t.Fatal("unsupported synthetic compiler command")
}

func productionFixture(t *testing.T) (*Compiler, string) {
	t.Helper()
	opts, _ := approvalFixture(t)
	if err := os.WriteFile(filepath.Join(opts.Root, "go.mod"), []byte("module example.com/source\n\ngo 1.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(opts.Root, "examples/gosx-docs"), 0700); err != nil {
		t.Fatal(err)
	}
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
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(opts.Root, ".git/production-helper.json")
	data, _ := json.Marshal(productionHelperConfig{Go: goPath})
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "go"), helperScript(executable, configPath), 0755); err != nil {
		t.Fatal(err)
	}
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

func TestProductionRejectsUntrackedBuildInputs(t *testing.T) {
	for _, name := range []string{"cmd/gosx/extra.go", "examples/gosx-docs/app/extra/page.server.go", "vendor/extra/extra.go"} {
		for _, ignored := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/ignored=%t", name, ignored), func(t *testing.T) {
				c, sha := productionFixture(t)
				path := filepath.Join(c.SourceRoot, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("package extra\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if ignored {
					if err := os.WriteFile(filepath.Join(c.SourceRoot, ".git/info/exclude"), []byte(name+"\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				scratch := t.TempDir()
				compiler, err := BuildCompiler(context.Background(), c.SourceRoot, sha, scratch)
				if compiler != nil {
					defer compiler.Close()
				}
				var typed *budget.InputError
				if compiler != nil || !errors.As(err, &typed) || typed.Code != "wrong-fixture" || typed.Pointer != "/production/source" {
					t.Fatal("untracked compiler input accepted", err)
				}
				for _, app := range []string{"docs", "scaffold"} {
					built, err := c.BuildApplication(context.Background(), app, scratch)
					if built != nil {
						defer built.Close()
					}
					if built != nil || !errors.As(err, &typed) || typed.Pointer != "/production/source" {
						t.Fatal("untracked application input accepted", app, err)
					}
				}
				if entries, err := os.ReadDir(scratch); err != nil || len(entries) != 0 {
					t.Fatal("compiler or fixture started before rejecting source", err)
				}
			})
		}
	}
}

func TestProductionEnvironmentPinsBuildAndRuntimeInputs(t *testing.T) {
	for _, key := range []string{"GOSX_DEV", "GOSX_ENV", "GOSX_RUNTIME_MODE", "GOSX_TINYGO_FULL_RUNTIME", "GOSX_RUNTIME_RELEASE_REPO", "GOSX_GO_WASM_OPT", "GOSX_DISABLE_SURFACE_AUTO", "GO_ENV", "NODE_ENV", "GOFLAGS", "GOEXPERIMENT", "CGO_ENABLED", "GOOS", "GOARCH", "GOROOT", "GOAMD64", "GOWORK", "GOENV", "GOTOOLCHAIN", "GODEBUG", "GOGC", "GOMEMLIMIT", "LD_PRELOAD", "APP_NAME", "HTTP_PROXY", "HTTPS_PROXY", "GIT_DIR", "GIT_CONFIG_COUNT"} {
		t.Setenv(key, "inherited")
	}
	got := map[string]string{}
	for _, item := range productionEnvironment(map[string]string{}) {
		key, value, _ := strings.Cut(item, "=")
		got[key] = value
	}
	for key, want := range map[string]string{"GOSX_DEV": "", "GOSX_ENV": "production", "GOSX_RUNTIME_MODE": "tinygo", "GO_ENV": "production", "NODE_ENV": "production", "GOWORK": "off", "GOENV": "off", "GOTOOLCHAIN": "local", "CGO_ENABLED": "0", "GOFLAGS": "-mod=readonly", "GOOS": runtime.GOOS, "GOARCH": runtime.GOARCH, "GIT_CONFIG_COUNT": "2", "GIT_CONFIG_VALUE_0": "false", "GIT_CONFIG_VALUE_1": "false"} {
		if got[key] != want {
			t.Errorf("%s was not pinned", key)
		}
	}
	for _, key := range []string{"GOSX_TINYGO_FULL_RUNTIME", "GOSX_RUNTIME_RELEASE_REPO", "GOSX_GO_WASM_OPT", "GOSX_DISABLE_SURFACE_AUTO", "GOEXPERIMENT", "GOROOT", "GOAMD64", "GODEBUG", "GOGC", "GOMEMLIMIT", "LD_PRELOAD", "APP_NAME", "HTTP_PROXY", "HTTPS_PROXY", "GIT_DIR"} {
		if _, exists := got[key]; exists {
			t.Errorf("%s was inherited", key)
		}
	}
	for _, key := range []string{"GOCACHE", "GOTMPDIR", "TMPDIR", "GOMODCACHE"} {
		if got[key] != os.Getenv(key) {
			t.Errorf("%s was not preserved", key)
		}
	}
}

func TestProductionRejectsHiddenTrackedChanges(t *testing.T) {
	for _, flag := range []string{"--assume-unchanged", "--skip-worktree"} {
		t.Run(flag, func(t *testing.T) {
			c, sha := productionFixture(t)
			gitFixture(t, c.SourceRoot, "update-index", flag, "budget.json")
			if err := os.WriteFile(filepath.Join(c.SourceRoot, "budget.json"), []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := BuildCompiler(context.Background(), c.SourceRoot, sha, t.TempDir())
			if got != nil {
				defer got.Close()
			}
			var typed *budget.InputError
			if got != nil || !errors.As(err, &typed) || typed.Pointer != "/production/source" {
				t.Fatal("index flags hid a changed source input", err)
			}
		})
	}
}

func TestProductionRejectsExternalAndVendorInputs(t *testing.T) {
	for _, kind := range []string{"replace", "vendor", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := productionFixture(t)
			pointer := "/production/module"
			switch kind {
			case "replace":
				mod := "module example.com/source\n\ngo 1.26\n\nreplace example.com/external => " + filepath.ToSlash(t.TempDir()) + "\n"
				if err := os.WriteFile(filepath.Join(c.SourceRoot, "go.mod"), []byte(mod), 0600); err != nil {
					t.Fatal(err)
				}
			case "vendor":
				if err := os.MkdirAll(filepath.Join(c.SourceRoot, "vendor"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(c.SourceRoot, "vendor/modules.txt"), []byte("# example.com/external v1.0.0\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				pointer = "/production/source"
				if err := os.Symlink(t.TempDir(), filepath.Join(c.SourceRoot, "external")); err != nil {
					t.Fatal(err)
				}
			}
			gitFixture(t, c.SourceRoot, "add", ".")
			gitFixture(t, c.SourceRoot, "commit", "-qm", "Add synthetic build input")
			sha := gitFixture(t, c.SourceRoot, "rev-parse", "HEAD")
			got, err := BuildCompiler(context.Background(), c.SourceRoot, sha, t.TempDir())
			if got != nil {
				defer got.Close()
			}
			var typed *budget.InputError
			if got != nil || !errors.As(err, &typed) || typed.Pointer != pointer {
				t.Fatal("unbound build input accepted", err)
			}
		})
	}
}

func TestProductionInheritedDevelopmentStillChecksReadiness(t *testing.T) {
	c, _ := productionFixture(t)
	a, err := c.BuildApplication(context.Background(), "docs", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	t.Setenv("GOSX_DEV", "1")
	t.Setenv("GOSX_ENV", "development")
	configureProductionHelper(t, c, func(config *productionHelperConfig) { config.Unready = true })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s, err := a.Serve(ctx, filepath.Join(t.TempDir(), "readiness.log"))
	if s != nil {
		defer s.Close()
	}
	var typed *budget.InputError
	if s != nil || !errors.As(err, &typed) || typed.Pointer != "/production/readiness" {
		t.Fatal("development skipped production readiness", err)
	}
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
	configureProductionHelper(t, c, func(config *productionHelperConfig) { config.BadManifest = true })
	if _, err := c.BuildApplication(context.Background(), "scaffold", t.TempDir()); err == nil {
		t.Fatal("legacy manifest accepted as measured evidence")
	}
}

func TestProductionServerReadinessIsolationAndCleanup(t *testing.T) {
	for _, app := range []string{"docs", "scaffold"} {
		t.Run(app, func(t *testing.T) {
			t.Setenv("GOSX_DEV", "1")
			t.Setenv("GOSX_ENV", "development")
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
				Gowork, Root, URL, Mode, Dev, Cache, Temp, GoTemp string
				SecretReady                                       bool
			}
			if json.Unmarshal(data, &got) != nil || got.Gowork != "off" || got.Root != a.DistDir || got.URL != s.BaseURL || !got.SecretReady || got.Mode != "production" || got.Dev != "" || got.Cache != os.Getenv("GOCACHE") || got.Temp != os.Getenv("TMPDIR") || got.GoTemp != os.Getenv("GOTMPDIR") || !strings.HasPrefix(s.BaseURL, "http://127.0.0.1:") {
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
	c, _ := productionFixture(t)
	executable, _ := os.Executable()
	childPID := filepath.Join(t.TempDir(), "child.pid")
	config := configureProductionHelper(t, c, func(config *productionHelperConfig) { config.ChildPID = childPID })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- productionCommand(ctx, t.TempDir(), filepath.Join(t.TempDir(), "build.log"), executable, "-test.run=^TestProductionHelper$", "--", config, "child-build")
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
	config := configureProductionHelper(t, c, func(config *productionHelperConfig) { config.ChildPID = childPID })
	if err := productionCommand(context.Background(), t.TempDir(), filepath.Join(t.TempDir(), "build.log"), executable, "-test.run=^TestProductionHelper$", "--", config, "child-build", "abandon"); err != nil {
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
	configureProductionHelper(t, c, func(config *productionHelperConfig) { config.Unready = true })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s, err := a.Serve(ctx, filepath.Join(t.TempDir(), "unready.log"))
	var typed *budget.InputError
	if s != nil || !errors.As(err, &typed) || typed.Pointer != "/production/readiness" || typed.Code != "timeout" && typed.Code != "environment" {
		t.Fatal("unready server became measurement evidence", err)
	}
}
