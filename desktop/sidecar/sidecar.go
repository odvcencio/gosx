// Package sidecar runs a helper process next to a desktop app: a local game
// server, a language server, or an audio engine such as `cicada studio`.
//
// Start launches the process without a console window, copies its output to
// a log, waits until it is ready, and ties its lifetime to the app.
//
// What ends with the app differs by platform:
//
//   - Windows: the sidecar and every process it starts join a Job Object
//     that the kernel kills when the app exits, including after a crash.
//   - Linux: Stop, and a sidecar that exits on its own, kill the sidecar's
//     whole process group. If the app itself crashes, the kernel sends
//     SIGKILL to the sidecar (a parent-death signal), but not to processes
//     the sidecar started; Linux has no unprivileged equivalent of a Job
//     Object. Sidecars that start their own children should set a
//     parent-death signal on them (prctl PR_SET_PDEATHSIG) or not start any.
//   - Other Unix systems: Stop and a sidecar exit kill the process group;
//     an app crash leaves the sidecar running.
//
// Readiness is either a line on standard output that matches ReadyLine (the
// first submatch, such as a listen address, is returned by Ready) or an HTTP
// URL that answers with a status below 500.
package sidecar

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

// DefaultReadyTimeout bounds Start when Options.ReadyTimeout is zero.
const DefaultReadyTimeout = 30 * time.Second

// ErrExited reports that the process exited before it became ready.
var ErrExited = errors.New("sidecar exited before it was ready")

// Options configures a sidecar process.
type Options struct {
	// Path is the executable. Args are its arguments (without Path).
	Path string
	Args []string
	// Dir is the working directory; empty uses the app's.
	Dir string
	// Env holds extra KEY=VALUE entries added to the app's environment.
	Env []string
	// Output receives the process's standard output and standard error,
	// line by line. Nil discards them. A log file opened for append is the
	// usual choice.
	Output io.Writer
	// ReadyLine marks the process ready when a standard-output line
	// matches. When the expression has a submatch, the first one is
	// returned by Process.Ready.
	ReadyLine *regexp.Regexp
	// ReadyURL marks the process ready when an HTTP GET returns a status
	// below 500. It is polled every ReadyPollInterval.
	ReadyURL string
	// ReadyPollInterval defaults to 100 ms.
	ReadyPollInterval time.Duration
	// ReadyTimeout bounds the wait for readiness; zero uses
	// DefaultReadyTimeout.
	ReadyTimeout time.Duration
	// ShowConsole keeps the Windows console window for console programs.
	// The default hides it.
	ShowConsole bool
}

// Process is a running sidecar.
type Process struct {
	cmd      *exec.Cmd
	ready    string
	done     chan struct{}
	waitErr  error
	stopOnce sync.Once
	platform *platformProcess
}

// Start launches the sidecar and returns once it is ready. If readiness
// fails, the process is stopped before Start returns. Cancelling ctx during
// the wait stops the process as well; after Start returns, ctx no longer
// affects it.
func Start(ctx context.Context, options Options) (*Process, error) {
	if options.Path == "" {
		return nil, errors.New("sidecar: path is empty")
	}
	if options.ReadyLine == nil && options.ReadyURL == "" {
		return nil, errors.New("sidecar: set ReadyLine or ReadyURL")
	}
	timeout := options.ReadyTimeout
	if timeout <= 0 {
		timeout = DefaultReadyTimeout
	}
	interval := options.ReadyPollInterval
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}

	cmd := exec.Command(options.Path, options.Args...)
	cmd.Dir = options.Dir
	cmd.Env = append(os.Environ(), options.Env...)
	configureCommand(cmd, options)
	output := options.Output
	if output == nil {
		output = io.Discard
	}
	// Use plain OS pipes rather than exec's copying writers, so cmd.Wait
	// returns when the main process exits even if a descendant still holds
	// the pipes open; release then ends those descendants.
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("sidecar: stdout pipe: %w", err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		stdoutRead.Close()
		stdoutWrite.Close()
		return nil, fmt.Errorf("sidecar: stderr pipe: %w", err)
	}
	cmd.Stdout = stdoutWrite
	cmd.Stderr = stderrWrite
	startErr := cmd.Start()
	// The child holds its own copies of the write ends.
	stdoutWrite.Close()
	stderrWrite.Close()
	if startErr != nil {
		stdoutRead.Close()
		stderrRead.Close()
		return nil, fmt.Errorf("sidecar: start %s: %w", options.Path, startErr)
	}
	p := &Process{cmd: cmd, done: make(chan struct{})}
	p.platform, err = attachProcess(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		stdoutRead.Close()
		stderrRead.Close()
		return nil, fmt.Errorf("sidecar: attach %s: %w", options.Path, err)
	}

	out := &lockedWriter{w: output}
	lineReady := make(chan string, 1)
	var copied sync.WaitGroup
	copied.Add(2)
	go func() {
		defer copied.Done()
		defer stdoutRead.Close()
		copyLines(stdoutRead, out, options.ReadyLine, lineReady)
	}()
	go func() {
		defer copied.Done()
		defer stderrRead.Close()
		copyLines(stderrRead, out, nil, nil)
	}()
	go func() {
		p.waitErr = cmd.Wait()
		p.platform.release()
		// Give the copiers a moment to drain what the process wrote before
		// it exited; release has ended any descendant holding the pipes.
		copiedDone := make(chan struct{})
		go func() { copied.Wait(); close(copiedDone) }()
		select {
		case <-copiedDone:
		case <-time.After(2 * time.Second):
		}
		close(p.done)
	}()

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ready, err := waitReady(waitCtx, options, interval, lineReady, p.done)
	if err != nil {
		_ = p.Stop(2 * time.Second)
		if errors.Is(err, ErrExited) && p.waitErr != nil {
			return nil, fmt.Errorf("%w: %v", ErrExited, p.waitErr)
		}
		return nil, err
	}
	p.ready = ready
	return p, nil
}

func waitReady(ctx context.Context, options Options, interval time.Duration, lineReady <-chan string, done <-chan struct{}) (string, error) {
	value := options.ReadyURL
	if options.ReadyLine != nil {
		select {
		case value = <-lineReady:
		case <-done:
			return "", ErrExited
		case <-ctx.Done():
			return "", fmt.Errorf("sidecar: no ready line: %w", ctx.Err())
		}
	}
	if options.ReadyURL == "" {
		return value, nil
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if probeURL(ctx, options.ReadyURL) {
			return value, nil
		}
		select {
		case <-ticker.C:
		case <-done:
			return "", ErrExited
		case <-ctx.Done():
			return "", fmt.Errorf("sidecar: %s not ready: %w", options.ReadyURL, ctx.Err())
		}
	}
}

func probeURL(ctx context.Context, url string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	client := http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	_ = response.Body.Close()
	return response.StatusCode < 500
}

// copyLines copies r to w line by line and sends the first ReadyLine match.
func copyLines(r io.Reader, w io.Writer, ready *regexp.Regexp, matched chan<- string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	sent := ready == nil
	for scanner.Scan() {
		line := scanner.Bytes()
		_, _ = w.Write(append(append([]byte(nil), line...), '\n'))
		if !sent {
			if match := ready.FindSubmatch(line); match != nil {
				value := string(match[0])
				if len(match) > 1 {
					value = string(match[1])
				}
				matched <- value
				sent = true
			}
		}
	}
	// Drain anything a scanner error left so the child never blocks.
	_, _ = io.Copy(w, r)
}

// Ready returns the ReadyLine submatch (or whole match), or ReadyURL when
// readiness came from the URL alone.
func (p *Process) Ready() string { return p.ready }

// PID returns the operating-system process ID.
func (p *Process) PID() int { return p.cmd.Process.Pid }

// Done is closed when the process has exited.
func (p *Process) Done() <-chan struct{} { return p.done }

// Wait blocks until the process exits and returns its exit error.
func (p *Process) Wait() error {
	<-p.done
	return p.waitErr
}

// Stop asks the process to exit, waits up to grace, and then kills whatever
// is left: the process itself and any children it started in the same
// process group (Unix) or Job Object (Windows), even when the main process
// already exited during the grace period. It returns once the process has
// exited. Stopping an exited process is a no-op.
func (p *Process) Stop(grace time.Duration) error {
	p.stopOnce.Do(func() {
		select {
		case <-p.done:
			return
		default:
		}
		if grace > 0 && p.platform.interrupt() == nil {
			select {
			case <-p.done:
			case <-time.After(grace):
			}
		}
		// Kill even if the main process exited: a child that ignored the
		// interrupt must not outlive Stop. After the main process is
		// reaped, release has already ended the group or job.
		p.platform.kill()
	})
	<-p.done
	return nil
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
