package metric

import (
	"context"
	"errors"
	"io"
	"strconv"
)

const PrometheusContentType = "text/plain; version=0.0.4; charset=utf-8"
const maxTextBytes = 2 << 20
const maxTextLine = 3072 // validated descriptor limits fit one 4 KiB chunk

// WritePrometheus emits deterministic text 0.0.4. It shares snapshot admission
// and bounded scratch with WithSnapshot. Output size is checked before the
// first write; caller I/O retains the lease until it returns. HTTP owners must
// apply their response deadline and buffer successful output before headers.
func (r *Registry) WritePrometheus(w io.Writer) error {
	if w == nil {
		return invalid("prometheus.writer", "required")
	}
	s := r.get()
	if s == nil {
		return nil
	}
	if err := s.acquireSnapshot(context.Background()); err != nil {
		return err
	}
	defer func() { <-s.snapshotGate }()
	snapshot := s.copySnapshot()
	// The dry pass guarantees oversized output cannot leak partial text.
	// Both passes reuse one fixed chunk; no second snapshot is allocated.
	count := textOutput{buf: s.textScratch[:0]}
	if err := count.render(snapshot); err != nil {
		return err
	}
	output := textOutput{writer: w, buf: s.textScratch[:0]}
	return output.render(snapshot)
}

type textOutput struct {
	writer io.Writer
	buf    []byte
	total  int
	err    error
}

func (o *textOutput) flush() {
	if o.err != nil || len(o.buf) == 0 {
		return
	}
	if len(o.buf) > maxTextBytes-o.total {
		o.err = ErrCapacity
		return
	}
	o.total += len(o.buf)
	if o.writer != nil {
		n, err := writeTextChunk(o.writer, o.buf)
		if err == nil && n != len(o.buf) {
			err = io.ErrShortWrite
		}
		if err != nil {
			o.err = &textWriteError{cause: err}
			return
		}
	}
	o.buf = o.buf[:0]
}

func (o *textOutput) startLine() bool {
	if cap(o.buf)-len(o.buf) < maxTextLine {
		o.flush()
	}
	return o.err == nil
}

func (o *textOutput) render(snapshot Snapshot) error {
	for _, f := range snapshot.Families {
		if !o.startLine() {
			return o.err
		}
		o.buf = append(o.buf, "# HELP "...)
		o.buf = append(o.buf, f.Name...)
		if f.Help != "" {
			o.buf = append(o.buf, ' ')
		}
		o.buf = appendEscaped(o.buf, f.Help, false)
		o.buf = append(o.buf, '\n')
		if !o.startLine() {
			return o.err
		}
		o.buf = append(o.buf, "# TYPE "...)
		o.buf = append(o.buf, f.Name...)
		o.buf = append(o.buf, ' ')
		kind := "counter"
		if f.Kind == KindGauge {
			kind = "gauge"
		} else if f.Kind == KindHistogram {
			kind = "histogram"
		}
		o.buf = append(o.buf, kind...)
		o.buf = append(o.buf, '\n')
		for _, s := range f.Series {
			if f.Kind != KindHistogram {
				if !o.sample(f.Name, "", s.Labels, false, 0, false) {
					return o.err
				}
				if f.Kind == KindCounter {
					o.buf = strconv.AppendUint(o.buf, s.Counter, 10)
				} else {
					o.buf = strconv.AppendFloat(o.buf, s.Gauge, 'g', -1, 64)
				}
				o.buf = append(o.buf, '\n')
				continue
			}
			h := s.Histogram
			var cumulative uint64
			for i, n := range h.Counts {
				cumulative += n
				infinity := i == len(h.Bounds)
				bound := 0.0
				if !infinity {
					bound = h.Bounds[i]
				}
				if !o.sample(f.Name, "_bucket", s.Labels, true, bound, infinity) {
					return o.err
				}
				o.buf = strconv.AppendUint(o.buf, cumulative, 10)
				o.buf = append(o.buf, '\n')
			}
			if !o.sample(f.Name, "_sum", s.Labels, false, 0, false) {
				return o.err
			}
			o.buf = strconv.AppendFloat(o.buf, h.Sum, 'g', -1, 64)
			o.buf = append(o.buf, '\n')
			if !o.sample(f.Name, "_count", s.Labels, false, 0, false) {
				return o.err
			}
			o.buf = strconv.AppendUint(o.buf, h.Count, 10)
			o.buf = append(o.buf, '\n')
		}
		if o.err != nil {
			return o.err
		}
	}
	o.flush()
	return o.err
}

func (o *textOutput) sample(name, suffix string, labels []LabelValue, bucket bool, bound float64, infinity bool) bool {
	if !o.startLine() {
		return false
	}
	o.buf = append(o.buf, name...)
	o.buf = append(o.buf, suffix...)
	if len(labels) > 0 || bucket {
		o.buf = append(o.buf, '{')
		for i, label := range labels {
			if i > 0 {
				o.buf = append(o.buf, ',')
			}
			o.buf = append(o.buf, label.Name...)
			o.buf = append(o.buf, '=', '"')
			o.buf = appendEscaped(o.buf, label.Value, true)
			o.buf = append(o.buf, '"')
		}
		if bucket {
			if len(labels) > 0 {
				o.buf = append(o.buf, ',')
			}
			o.buf = append(o.buf, "le=\""...)
			if infinity {
				o.buf = append(o.buf, "+Inf"...)
			} else {
				o.buf = strconv.AppendFloat(o.buf, bound, 'g', -1, 64)
			}
			o.buf = append(o.buf, '"')
		}
		o.buf = append(o.buf, '}')
	}
	o.buf = append(o.buf, ' ')
	return true
}

func appendEscaped(dst []byte, value string, quote bool) []byte {
	for i := range len(value) {
		switch value[i] {
		case '\\':
			dst = append(dst, '\\', '\\')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '"':
			if quote {
				dst = append(dst, '\\')
			}
			dst = append(dst, '"')
		default:
			dst = append(dst, value[i])
		}
	}
	return dst
}

type textWriteError struct{ cause error }

var errTextPanic = errors.New("metric: exposition writer panic")

func writeTextChunk(w io.Writer, p []byte) (n int, err error) {
	defer func() {
		if recover() != nil {
			err = errTextPanic
		}
	}()
	return w.Write(p)
}

func (e *textWriteError) Error() string { return "metric: exposition write failed" }
func (e *textWriteError) Unwrap() error { return e.cause }
