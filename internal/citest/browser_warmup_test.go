package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type fakeBrowserGoCall struct {
	Args []string
	Env  map[string]string
}

// The selected test executable acts as Go only in child processes. This keeps
// order, environment and fail-closed coverage portable without a shell shim.
func init() {
	if os.Getenv("GOSX_CITEST_FAKE_GO") != "1" {
		return
	}
	c := fakeBrowserGoCall{Args: os.Args[1:], Env: map[string]string{}}
	for _, k := range []string{"GOOS", "GOARCH", "GOWORK", "GOFLAGS", "GOCACHE", "GOMODCACHE", "GOTOOLCHAIN"} {
		c.Env[k] = os.Getenv(k)
	}
	f, err := os.OpenFile(os.Getenv("GOSX_CITEST_FAKE_GO_LOG"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(90)
	}
	if json.NewEncoder(f).Encode(c) != nil {
		os.Exit(91)
	}
	f.Close()
	if len(c.Args) > 0 && c.Args[0] == "build" && c.Args[len(c.Args)-1] == "./client/wasm" && os.Getenv("GOSX_CITEST_FAKE_GO_FAIL_WASM") == "1" {
		os.Exit(7)
	}
	if len(c.Args) > 0 && c.Args[0] == "test" && strings.Contains(strings.Join(c.Args, " "), "-list") {
		names := make([]string, 0, len(browserLayout.pins))
		for n := range browserLayout.pins {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			os.Stdout.WriteString(n + "\n")
		}
	}
	os.Exit(0)
}

func TestBrowserShardWarmupOrderEnvironmentAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "wasm-warmup-failure"
		}
		t.Run(name, func(t *testing.T) {
			log := filepath.Join(t.TempDir(), "calls.jsonl")
			if err := os.WriteFile(log, nil, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOSX_CITEST_FAKE_GO", "1")
			t.Setenv("GOSX_CITEST_FAKE_GO_LOG", log)
			t.Setenv("GOSX_CITEST_FAKE_GO_FAIL_WASM", map[bool]string{false: "0", true: "1"}[fail])
			inherited := map[string]string{"GOOS": "host-fixture", "GOARCH": "host-arch", "GOWORK": "fixture.work", "GOFLAGS": "-tags=fixture", "GOCACHE": "fixture-build-cache", "GOMODCACHE": "fixture-module-cache", "GOTOOLCHAIN": "go1.26.0"}
			for k, v := range inherited {
				t.Setenv(k, v)
			}
			goTool, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			err = runBrowserShard([]string{"2"}, goTool, &output, &output)
			if fail {
				if err == nil || !strings.Contains(err.Error(), "warm build cache ./client/wasm") {
					t.Fatalf("want fatal WASM preparation failure, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(log)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			dec := json.NewDecoder(f)
			var calls []fakeBrowserGoCall
			for {
				var c fakeBrowserGoCall
				err = dec.Decode(&c)
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				calls = append(calls, c)
			}
			wantCount := 5
			if fail {
				wantCount = 3
			}
			if len(calls) != wantCount {
				t.Fatalf("got %d calls, want %d: %+v", len(calls), wantCount, calls)
			}
			for i, pkg := range []string{"./cmd/gosx", "./examples/gosx-docs", "./client/wasm"} {
				if !reflect.DeepEqual(calls[i].Args, []string{"build", "-o", os.DevNull, pkg}) {
					t.Fatalf("warmup %d args: %v", i, calls[i].Args)
				}
				wantEnv := map[string]string{}
				for k, v := range inherited {
					wantEnv[k] = v
				}
				if i == 2 {
					wantEnv["GOOS"] = "js"
					wantEnv["GOARCH"] = "wasm"
					wantEnv["GOWORK"] = "off"
					wantEnv["GOFLAGS"] = "-mod=mod -buildvcs=false"
				}
				if !reflect.DeepEqual(calls[i].Env, wantEnv) {
					t.Fatalf("warmup %d env: got %v want %v", i, calls[i].Env, wantEnv)
				}
			}
			if !fail {
				if !strings.Contains(strings.Join(calls[3].Args, " "), "-list . ./e2e") {
					t.Fatalf("discovery not after warmups: %v", calls[3].Args)
				}
				if !strings.Contains(strings.Join(calls[4].Args, " "), "-timeout 30m") {
					t.Fatalf("execution flags changed: %v", calls[4].Args)
				}
				for i := 3; i < 5; i++ {
					if !reflect.DeepEqual(calls[i].Env, inherited) {
						t.Fatalf("WASM env leaked into native test call %d: %v", i, calls[i].Env)
					}
				}
			}
		})
	}
}
