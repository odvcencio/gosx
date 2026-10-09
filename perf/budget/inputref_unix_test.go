//go:build unix

package budget

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestInputRejectsFIFOWithoutWriter(t *testing.T) {
	if path := os.Getenv("GOSX_TEST_INPUT_FIFO"); path != "" {
		if _, err := LoadProfile(path, LoadOptions{RootDir: filepath.Dir(path)}); err == nil {
			t.Fatal("FIFO accepted as an input file")
		}
		return
	}
	path := filepath.Join(t.TempDir(), "input.fifo")
	if err := unix.Mkfifo(path, 0600); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.ENOSYS) {
			t.Skip("temporary filesystem does not support FIFOs")
		}
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestInputRejectsFIFOWithoutWriter$")
	command.Env = append(os.Environ(), "GOSX_TEST_INPUT_FIFO="+path)
	out, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("opening an input FIFO blocked without a writer")
	}
	if err != nil {
		t.Fatalf("FIFO rejection failed: %s: %v", out, err)
	}
}

func TestMeasureRejectsFIFOWithoutWriter(t *testing.T) {
	if path := os.Getenv("GOSX_TEST_MEASURE_FIFO"); path != "" {
		root, err := os.OpenRoot(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		var typed *InputError
		if _, err := readMeasureFile(root, filepath.Base(path), maxMeasureBody); !errors.As(err, &typed) || typed.Code != "wrong-fixture" {
			t.Fatal("FIFO accepted as a measured file", err)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "asset.js")
	if err := unix.Mkfifo(path, 0600); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.ENOSYS) {
			t.Skip("temporary filesystem does not support FIFOs")
		}
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestMeasureRejectsFIFOWithoutWriter$")
	command.Env = append(os.Environ(), "GOSX_TEST_MEASURE_FIFO="+path)
	out, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("opening a measured FIFO blocked without a writer")
	}
	if err != nil {
		t.Fatalf("FIFO rejection failed: %s: %v", out, err)
	}
}
