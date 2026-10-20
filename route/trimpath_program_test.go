package route

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// gosx#239: LoadFileProgramHere and RegisterFileModuleHere must resolve a
// page's sibling .gsx file correctly in a `-trimpath` binary, not only in
// `go test` and `go run`, because neither of those ever passes -trimpath —
// a unit test that only exercises this package in-process cannot catch a
// regression here. These tests build and run real binaries instead.
//
// TestLoadFileProgramHereSurvivesTrimpathBuild is the end-to-end proof: it
// writes a tiny app that follows the documented "Rendering a fragment from a
// Go handler" pattern (gosx#226) with route.LoadFileProgramHere, builds it
// once with `go build -trimpath` (gosx build's own default) and once without
// it, runs each binary as a real subprocess, and asserts both the page and
// its fragment endpoint return 200 with the expected markup — proving dev
// mode and a `-trimpath` production binary both work, not just one.
func TestLoadFileProgramHereSurvivesTrimpathBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds real binaries; skipped in short mode")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not found on PATH")
	}

	repoRoot := trimpathTestRepoRoot(t)
	appRoot := writeTrimpathFixtureApp(t, repoRoot)

	buildDir := t.TempDir()
	trimmedBin := filepath.Join(buildDir, "trimtestapp-trimpath")
	plainBin := filepath.Join(buildDir, "trimtestapp-plain")

	buildTrimpathFixtureApp(t, goBin, appRoot, trimmedBin, true)
	buildTrimpathFixtureApp(t, goBin, appRoot, plainBin, false)

	t.Run("trimpath build", func(t *testing.T) {
		assertTrimpathFixtureAppServes(t, trimmedBin, appRoot)
	})
	t.Run("non-trimpath build", func(t *testing.T) {
		assertTrimpathFixtureAppServes(t, plainBin, appRoot)
	})
}

func trimpathTestRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve repo root caller")
	}
	// This file lives at <repoRoot>/route/trimpath_program_test.go.
	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}

// writeTrimpathFixtureApp writes a standalone module, outside the gosx
// module tree, that depends on gosx through a local replace directive and
// reproduces the documented fragment pattern: app/wire/page.gsx declares
// SignalCard and Page, and app/wire/page.server.go serves a fragment of
// SignalCard from route.LoadFileProgramHere("page.gsx") — a plain
// http.Handler in the same package as the page, same as the docs.
func writeTrimpathFixtureApp(t *testing.T, repoRoot string) string {
	t.Helper()
	root := t.TempDir()

	goMod := fmt.Sprintf(`module trimtestapp

go 1.25

require m31labs.dev/gosx v0.0.0-00010101000000-000000000000

replace m31labs.dev/gosx => %s
`, filepath.ToSlash(repoRoot))
	writeTrimpathFile(t, root, "go.mod", goMod)

	mainGo := `package main

import (
	"fmt"
	"log"
	"net"
	"net/http"

	"m31labs.dev/gosx/route"
	"trimtestapp/app/wire"
)

func main() {
	router := route.NewRouter()
	router.Handle("/wire/signal", http.HandlerFunc(wire.ServeSignalFragment))
	if err := router.AddDir("app", route.FileRoutesOptions{}); err != nil {
		log.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		log.Fatalf("BuildChecked: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	fmt.Printf("LISTENING %s\n", ln.Addr().String())
	log.Fatal(http.Serve(ln, handler))
}
`
	writeTrimpathFile(t, root, "main.go", mainGo)

	pageGSX := `package wire

type SignalCardProps struct {
	Label string
	Value string
}

component SignalCard(props: SignalCardProps) {
	return <li class="signal-card">{props.Label}: {props.Value}</li>
}

component Page() {
	return <ul data-gosx-region data-gosx-region-url="/wire/signal">
		<SignalCard label="Passing Yards" value="317" />
	</ul>
}
`
	writeTrimpathFile(t, root, "app/wire/page.gsx", pageGSX)

	pageServerGo := `package wire

import "net/http"

import "m31labs.dev/gosx/route"

// SignalCardProps mirrors page.gsx's declared props struct by field
// coverage, not by name (a same-shaped struct under a different Go type
// name satisfies the strict boundary by design).
type SignalCardProps struct {
	Label string
	Value string
}

// ServeSignalFragment is the "Rendering a fragment from a Go handler"
// pattern from the docs, gosx#226: a plain http.Handler in the same package
// as page.gsx, loading that file's own compiled program with
// route.LoadFileProgramHere and rendering one of its components directly.
func ServeSignalFragment(w http.ResponseWriter, r *http.Request) {
	prog, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	html, err := route.RenderProgramComponent(prog, "SignalCard", route.ProgramRenderEnv{
		Props: SignalCardProps{Label: "Passing Yards", Value: "317"},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}
`
	writeTrimpathFile(t, root, "app/wire/page.server.go", pageServerGo)

	tidyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	tidy := exec.CommandContext(tidyCtx, "go", "mod", "tidy")
	tidy.Dir = root
	tidy.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}

	return root
}

func writeTrimpathFile(t *testing.T, root, rel, contents string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func buildTrimpathFixtureApp(t *testing.T, goBin, appRoot, outputPath string, trimpath bool) {
	t.Helper()
	args := []string{"build", "-buildvcs=false"}
	if trimpath {
		args = append(args, "-trimpath")
	}
	args = append(args, "-o", outputPath, ".")

	buildCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(buildCtx, goBin, args...)
	cmd.Dir = appRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build (trimpath=%v): %v\n%s", trimpath, err, out)
	}
}

// assertTrimpathFixtureAppServes runs binPath as a subprocess with its
// working directory set to appRoot — the same shape a container gives a
// deployed binary (WORKDIR set to the app root; the binary itself can live
// anywhere) — and proves both the page and its fragment endpoint render,
// rather than 500.
func assertTrimpathFixtureAppServes(t *testing.T, binPath, appRoot string) {
	t.Helper()

	cmd := exec.Command(binPath)
	cmd.Dir = appRoot
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", binPath, err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	addrCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if addr, ok := strings.CutPrefix(line, "LISTENING "); ok {
				addrCh <- addr
			}
		}
	}()

	var addr string
	select {
	case addr = <-addrCh:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never printed its listening address", binPath)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	base := "http://" + addr

	pageResp, err := client.Get(base + "/wire")
	if err != nil {
		t.Fatalf("GET /wire: %v", err)
	}
	pageBody, _ := readAllAndClose(pageResp.Body)
	if pageResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /wire = %d, want 200; body: %s", pageResp.StatusCode, pageBody)
	}
	if !strings.Contains(pageBody, `data-gosx-region-url="/wire/signal"`) {
		t.Fatalf("page is missing its data-gosx-region wiring: %s", pageBody)
	}

	fragResp, err := client.Get(base + "/wire/signal")
	if err != nil {
		t.Fatalf("GET /wire/signal: %v", err)
	}
	fragBody, _ := readAllAndClose(fragResp.Body)
	const wantFragment = `<li class="signal-card">Passing Yards: 317</li>`
	if fragResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /wire/signal = %d, want 200; body: %s", fragResp.StatusCode, fragBody)
	}
	if fragBody != wantFragment {
		t.Fatalf("fragment body = %q, want %q", fragBody, wantFragment)
	}
}

func readAllAndClose(body io.ReadCloser) (string, error) {
	defer body.Close()
	data, err := io.ReadAll(body)
	return string(data), err
}
