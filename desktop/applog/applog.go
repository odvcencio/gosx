// Package applog provides a small rotating file logger for desktop apps.
// Open creates a log that can be used directly as an io.Writer or through a
// slog.Logger.
package applog

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	defaultMaxBytes = 5 << 20
	defaultKeep     = 3
)

// ErrInvalidOptions indicates that log options are invalid.
var ErrInvalidOptions = errors.New("invalid applog options")

// Options configures a rotating log file. Name is the base file name without
// an extension; the active file is Dir/Name.log. A zero MaxBytes defaults to
// 5 MiB, and a zero Keep defaults to three rotated files.
type Options struct {
	Dir      string
	Name     string
	MaxBytes int64
	Keep     int
}

// Log is a concurrency-safe rotating file writer.
type Log struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	size     int64
	maxBytes int64
	keep     int
	closed   bool
}

// Open creates the log directory and opens the active log file for appending.
func Open(options Options) (*Log, error) {
	if options.Dir == "" || options.Name == "" ||
		strings.ContainsAny(options.Name, `/\`) || strings.Contains(options.Name, "..") ||
		options.MaxBytes < 0 || options.Keep < 0 {
		return nil, fmt.Errorf("%w: directory, name, or rotation limits are invalid", ErrInvalidOptions)
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = defaultMaxBytes
	}
	if options.Keep == 0 {
		options.Keep = defaultKeep
	}
	if err := os.MkdirAll(options.Dir, 0755); err != nil {
		return nil, fmt.Errorf("create applog directory: %w", err)
	}

	path := filepath.Join(options.Dir, options.Name+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open applog file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("stat applog file: %w", err)
	}
	return &Log{
		file:     file,
		path:     path,
		size:     info.Size(),
		maxBytes: options.MaxBytes,
		keep:     options.Keep,
	}, nil
}

// Write appends p to the active file, rotating it first when necessary.
func (l *Log) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, os.ErrClosed
	}
	if l.file == nil {
		// An earlier rotation failed and could not reopen the file; retry,
		// so a temporary filesystem error does not stop logging for good.
		if err := l.reopen(); err != nil {
			return 0, err
		}
	}
	if len(p) == 0 {
		return 0, nil
	}
	if l.size > 0 && int64(len(p)) > l.maxBytes-l.size {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.file.Write(p)
	l.size += int64(n)
	return n, err
}

// Close closes the active log file. Repeated calls are safe.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.file == nil {
		return nil
	}
	return l.file.Close()
}

// Path returns the path of the active log file.
func (l *Log) Path() string {
	return l.path
}

// Logger returns a text slog logger that writes to this log.
func (l *Log) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(l, nil))
}

func (l *Log) rotate() error {
	// Forget the file before handling a close error, so a later Write
	// retries reopening even if this recovery fails.
	closeErr := l.file.Close()
	l.file = nil
	if closeErr != nil {
		return l.reopenAfterRotationError(fmt.Errorf("close applog before rotation: %w", closeErr))
	}
	if err := os.Remove(l.rotatedPath(l.keep)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return l.reopenAfterRotationError(fmt.Errorf("remove oldest applog: %w", err))
	}
	for i := l.keep - 1; i >= 1; i-- {
		from := l.rotatedPath(i)
		if err := os.Rename(from, l.rotatedPath(i+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return l.reopenAfterRotationError(fmt.Errorf("rotate applog %d: %w", i, err))
		}
	}
	if err := os.Rename(l.path, l.rotatedPath(1)); err != nil {
		return l.reopenAfterRotationError(fmt.Errorf("rotate active applog: %w", err))
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return l.reopenAfterRotationError(fmt.Errorf("reopen applog after rotation: %w", err))
	}
	l.file = file
	l.size = 0
	return nil
}

// reopen opens the active path for appending and refreshes its size.
func (l *Log) reopen() error {
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("reopen applog: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("stat reopened applog: %w", err)
	}
	l.file = file
	l.size = info.Size()
	return nil
}

func (l *Log) rotatedPath(index int) string {
	extension := filepath.Ext(l.path)
	base := strings.TrimSuffix(l.path, extension)
	return fmt.Sprintf("%s.%d%s", base, index, extension)
}

func (l *Log) reopenAfterRotationError(rotationErr error) error {
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("%w; reopen applog: %v", rotationErr, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("%w; stat reopened applog: %v", rotationErr, err)
	}
	l.file = file
	l.size = info.Size()
	return rotationErr
}

var _ io.WriteCloser = (*Log)(nil)
