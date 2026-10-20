package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPerfBuildAppCLIFlags(t *testing.T) {
	if os.Getenv("GOSX_TEST_PERF_APP_CLI") == "1" {
		for i, arg := range os.Args {
			if arg == "--" {
				os.Args = append([]string{"gosx", "build"}, os.Args[i+1:]...)
				cmdBuild()
				return
			}
		}
		t.Fatal("missing subprocess arguments")
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--perf-app"}, "--perf-app requires an app ID"},
		{[]string{"--dev", "--perf-app", "fixture", "."}, "invalid-input at /perfAppID"},
		{[]string{"--prod", "--perf-app", "invalid-id?private", "."}, "invalid-input at /perfAppID"},
	} {
		args := append([]string{"-test.run=^TestPerfBuildAppCLIFlags$", "--"}, tc.args...)
		cmd := exec.Command(os.Args[0], args...)
		cmd.Env = append(os.Environ(), "GOSX_TEST_PERF_APP_CLI=1")
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), tc.want) || strings.Contains(string(out), "invalid-id?private") {
			t.Fatalf("performance build flag validation differs: %v: %s", err, out)
		}
	}
}
