package sidecar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const helperEnv = "GOSX_SIDECAR_TEST_HELPER"

// TestMain lets the test binary act as the sidecar: when helperEnv is set it
// runs the named helper mode instead of the tests.
func TestMain(m *testing.M) {
	if mode := os.Getenv(helperEnv); mode != "" {
		runHelper(mode)
		return
	}
	os.Exit(m.Run())
}

func runHelper(mode string) {
	switch mode {
	case "serve-line":
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "starting")
		fmt.Printf("Test Studio: http://%s/\n", ln.Addr())
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	case "serve-url":
		time.Sleep(200 * time.Millisecond)
		_ = http.ListenAndServe(os.Getenv("GOSX_SIDECAR_TEST_ADDR"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	case "exit":
		fmt.Println("bye")
		os.Exit(3)
	case "silent":
		time.Sleep(time.Minute)
	case "graceful":
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM, os.Interrupt)
		fmt.Println("ready")
		<-signals
		fmt.Println("graceful exit")
		os.Exit(0)
	case "host":
		// Act as a desktop app that starts a sidecar and then crashes
		// without calling Stop.
		options := Options{Path: os.Args[0], Env: []string{helperEnv + "=silent-ready"}, ReadyLine: regexp.MustCompile(`^ready`)}
		p, err := Start(context.Background(), options)
		if err != nil {
			os.Exit(2)
		}
		fmt.Printf("sidecar %d\n", p.PID())
		os.Exit(1)
	case "silent-ready":
		fmt.Println("ready")
		time.Sleep(time.Minute)
	case "ignore-term":
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(time.Minute)
	case "graceful-spawn":
		// Exits on SIGTERM but leaves a child that ignores SIGTERM.
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), helperEnv+"=ignore-term")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		fmt.Printf("child %d\n", child.Process.Pid)
		<-signals
		os.Exit(0)
	case "leak-stdout":
		// Leaves a child holding stdout open, then exits.
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), helperEnv+"=silent")
		child.Stdout = os.Stdout
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		fmt.Printf("ready %d\n", child.Process.Pid)
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	case "spawn":
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), helperEnv+"=silent")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		fmt.Printf("child %d\n", child.Process.Pid)
		time.Sleep(time.Minute)
	}
}

func helperOptions(mode string, output *syncBuffer) Options {
	options := Options{
		Path:         os.Args[0],
		Env:          []string{helperEnv + "=" + mode},
		ReadyTimeout: 10 * time.Second,
	}
	if output != nil {
		options.Output = output
	}
	return options
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestStartReadyLineReturnsSubmatchAndCopiesOutput(t *testing.T) {
	var output syncBuffer
	options := helperOptions("serve-line", &output)
	options.ReadyLine = regexp.MustCompile(`^Test Studio: (http://\S+)`)
	p, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Stop(0)
	if !strings.HasPrefix(p.Ready(), "http://127.0.0.1:") {
		t.Fatalf("Ready() = %q", p.Ready())
	}
	response, err := http.Get(p.Ready())
	if err != nil {
		t.Fatalf("GET ready address: %v", err)
	}
	response.Body.Close()
	if err := p.Stop(time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-p.Done():
	default:
		t.Fatal("Done not closed after Stop")
	}
	if got := output.String(); !strings.Contains(got, "starting\n") || !strings.Contains(got, "Test Studio: http://") {
		t.Fatalf("output = %q", got)
	}
}

func TestStartReadyURLPolls(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	options := helperOptions("serve-url", nil)
	options.Env = append(options.Env, "GOSX_SIDECAR_TEST_ADDR="+addr)
	options.ReadyURL = "http://" + addr + "/"
	options.ReadyPollInterval = 20 * time.Millisecond
	p, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Stop(0)
	if p.Ready() != options.ReadyURL {
		t.Fatalf("Ready() = %q, want %q", p.Ready(), options.ReadyURL)
	}
	if p.PID() <= 0 {
		t.Fatalf("PID() = %d", p.PID())
	}
}

func TestStartReportsEarlyExit(t *testing.T) {
	var output syncBuffer
	options := helperOptions("exit", &output)
	options.ReadyLine = regexp.MustCompile(`never`)
	_, err := Start(context.Background(), options)
	if !errors.Is(err, ErrExited) {
		t.Fatalf("Start error = %v, want ErrExited", err)
	}
	if !strings.Contains(output.String(), "bye") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestStartTimesOutAndStopsProcess(t *testing.T) {
	options := helperOptions("silent", nil)
	options.ReadyLine = regexp.MustCompile(`never`)
	options.ReadyTimeout = 300 * time.Millisecond
	started := time.Now()
	_, err := Start(context.Background(), options)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("Start took %v after timeout", elapsed)
	}
}

func TestStartRejectsMissingReadiness(t *testing.T) {
	if _, err := Start(context.Background(), Options{Path: os.Args[0]}); err == nil {
		t.Fatal("Start without ReadyLine or ReadyURL succeeded")
	}
	if _, err := Start(context.Background(), Options{ReadyURL: "http://127.0.0.1:1/"}); err == nil {
		t.Fatal("Start without Path succeeded")
	}
}

func TestDoneClosesWhenChildKeepsStdoutOpen(t *testing.T) {
	options := helperOptions("leak-stdout", nil)
	options.ReadyLine = regexp.MustCompile(`^ready (\d+)`)
	p, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(10 * time.Second):
		_ = p.Stop(0)
		t.Fatal("Done did not close after the main process exited while a child held stdout")
	}
	if err := p.Wait(); err != nil {
		t.Fatalf("Wait = %v, want nil", err)
	}
}
