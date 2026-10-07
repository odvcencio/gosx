package metric

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func goldenRegistry(t *testing.T) *Registry {
	t.Helper()
	r := &Registry{}
	g, _ := r.NewGauge(GaugeOptions{Name: "z_gauge"})
	gauge, _ := g.Bind()
	gauge.Set(-3.5)
	c, _ := r.NewCounter(CounterOptions{Name: "a_counter", Help: "Requests \\path\nwith \"quotes\"", Labels: []Label{{Name: "kind", Values: []string{"z", "a\"line\n\\"}}}})
	z, _ := c.Bind("z")
	z.Add(1)
	a, _ := c.Bind("a\"line\n\\")
	a.Add(2)
	v, _ := r.NewHistogram(HistogramOptions{Name: "latency", Help: "Complete work", Bounds: []float64{1, 2}})
	h, _ := v.Bind()
	for _, n := range []float64{-1, .5, 1.5, 4} {
		h.Observe(n)
	}
	r.Seal()
	return r
}

func TestPrometheusGoldenAndParser(t *testing.T) {
	r := goldenRegistry(t)
	var output bytes.Buffer
	if err := r.WritePrometheus(&output); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/prometheus.golden")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("golden mismatch:\n%s", output.Bytes())
	}
	if samples := parseText(t, output.String()); samples != 8 {
		t.Fatalf("samples=%d", samples)
	}
	if promtool, err := exec.LookPath("promtool"); err == nil {
		cmd := exec.Command(promtool, "check", "metrics")
		cmd.Stdin = bytes.NewReader(output.Bytes())
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("promtool: %v %s", err, output)
		}
	}
}

// This small test parser is independent of the production append formatter.
// It validates unique metadata/samples and label quoting/escapes, not merely
// reparsing numbers produced by strconv.AppendFloat.
func parseText(t *testing.T, text string) int {
	t.Helper()
	seen, help, types := map[string]bool{}, map[string]bool{}, map[string]bool{}
	n := 0
	if !strings.HasSuffix(text, "\n") || !utf8.ValidString(text) {
		t.Fatal("invalid text framing")
	}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if strings.HasPrefix(line, "# ") {
			parts := strings.SplitN(line, " ", 4)
			if len(parts) < 3 || parts[1] == "TYPE" && len(parts) != 4 {
				t.Fatal("metadata", line)
			}
			set := help
			if parts[1] == "TYPE" {
				set = types
			} else if parts[1] != "HELP" {
				t.Fatal("metadata type")
			}
			if set[parts[2]] {
				t.Fatal("duplicate metadata")
			}
			set[parts[2]] = true
			continue
		}
		quoted, escaped, split := false, false, -1
		for i, c := range line {
			if escaped {
				if c != '\\' && c != '"' && c != 'n' {
					t.Fatal("bad escape")
				}
				escaped = false
				continue
			}
			if quoted && c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				quoted = !quoted
			}
			if c == ' ' && !quoted {
				split = i
				break
			}
		}
		if quoted || escaped || split < 0 || seen[line[:split]] {
			t.Fatal("invalid or duplicate sample", line)
		}
		if _, err := strconv.ParseFloat(line[split+1:], 64); err != nil {
			t.Fatal(err)
		}
		seen[line[:split]] = true
		n++
	}
	return n
}

type failedWriter struct {
	cause  error
	short  bool
	panics bool
}

func (w failedWriter) Write(p []byte) (int, error) {
	if w.panics {
		panic("PRIVATE_WRITER_PANIC")
	}
	if w.short {
		return len(p) - 1, nil
	}
	return 0, w.cause
}

func TestPrometheusWriterFailuresAndLease(t *testing.T) {
	r := goldenRegistry(t)
	cause := errors.New("PRIVATE_IO_ERROR")
	err := r.WritePrometheus(failedWriter{cause: cause})
	if !errors.Is(err, cause) || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal(err)
	}
	if err := r.WritePrometheus(failedWriter{short: true}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	if err := r.WritePrometheus(failedWriter{panics: true}); !errors.Is(err, errTextPanic) {
		t.Fatal(err)
	}
	if err := r.WithSnapshot(context.Background(), func(Snapshot) error { return nil }); err != nil {
		t.Fatal("writer retained lease", err)
	}
}

type blockingWriter struct {
	entered, release chan struct{}
	once             sync.Once
	output           bytes.Buffer
}

func (w *blockingWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered); <-w.release })
	return w.output.Write(p)
}

func TestPrometheusWriterRetainsScratch(t *testing.T) {
	r := goldenRegistry(t)
	w := &blockingWriter{entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- r.WritePrometheus(w) }()
	<-w.entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := r.WithSnapshot(ctx, func(Snapshot) error { t.Fatal("reused writer scratch"); return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(w.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	parseText(t, w.output.String())
}
