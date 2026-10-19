//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The "-smoke" run of examples/desktop-app, as an integration test: build the
// example with CGO_ENABLED=0, check which frameworks the binary loads, run it
// (a window opens for about five seconds) and require that the page loaded, the
// startup timeline reported every phase this backend has, the process exited
// cleanly, and the sidecar did not outlive it. Skipped under -short and outside
// a GUI session, because it opens a real window.
func TestDarwinSmokeIntegration(t *testing.T) {
	requireGUI(t)
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go tool on PATH to build the example: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "desktop-app")
	build := exec.Command(goBin, "build", "-trimpath", "-o", bin, "../examples/desktop-app")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./examples/desktop-app: %v\n%s", err, out)
	}

	if otool, err := exec.LookPath("otool"); err == nil {
		out, err := exec.Command(otool, "-L", bin).CombinedOutput()
		if err != nil {
			t.Fatalf("otool -L: %v\n%s", err, out)
		}
		for _, want := range []string{"AppKit.framework", "WebKit.framework", "libobjc.A.dylib"} {
			if !strings.Contains(string(out), want) {
				t.Errorf("otool -L does not list %s:\n%s", want, out)
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var output bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "-smoke")
	cmd.Stdout, cmd.Stderr = &output, &output
	started := time.Now()
	if err := cmd.Run(); err != nil {
		t.Fatalf("desktop-app -smoke: %v\n%s", err, output.String())
	}
	t.Logf("-smoke ran for %v:\n%s", time.Since(started).Round(10*time.Millisecond), output.String())
	log := output.String()
	for _, bad := range []string{"fatal", "panic", "SIGSEGV", "unsupported"} {
		if strings.Contains(log, bad) {
			t.Errorf("-smoke output contains %q", bad)
		}
	}

	m := regexp.MustCompile(`navigation (\d+) success=true status=0; startup: window (\S+), web view (\S+), first page (\S+)`).FindStringSubmatch(log)
	if m == nil {
		t.Fatalf("no successful navigation with a startup timeline in the output")
	}
	for i, name := range []string{"WindowShown", "ControllerReady", "FirstNavigationCompleted"} {
		d, err := time.ParseDuration(m[2+i])
		if err != nil || d <= 0 {
			t.Errorf("%s = %q, want a positive duration", name, m[2+i])
		}
	}

	// The sidecar is gone: its port no longer accepts connections.
	e := regexp.MustCompile(`desktop-app engine: http://(127\.0\.0\.1:\d+)/`).FindStringSubmatch(log)
	if e == nil {
		t.Fatal("the engine never announced its address")
	}
	if conn, err := net.DialTimeout("tcp", e[1], time.Second); err == nil {
		conn.Close()
		t.Errorf("the engine at %s is still listening after the host exited", e[1])
	}
}
