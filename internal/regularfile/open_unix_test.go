//go:build unix

package regularfile

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

func TestOpenRejectsSymlinkReplacement(t *testing.T) {
	dir := t.TempDir()
	body := filepath.Join(dir, "body")
	if err := os.WriteFile(body, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, err := root.Lstat("body")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(body, filepath.Join(dir, "saved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("saved", body); err != nil {
		t.Fatal(err)
	}
	file, err := openChecked(root, "body", before)
	if file != nil {
		file.Close()
	}
	if err == nil || file != nil {
		t.Fatal("symlink replacement to the original inode was accepted")
	}
}

func TestOpenRejectsFIFOReplacementWithoutBlocking(t *testing.T) {
	if os.Getenv("GOSX_TEST_REGULAR_FIFO") == "1" {
		dir := t.TempDir()
		body := filepath.Join(dir, "body")
		if err := os.WriteFile(body, nil, 0600); err != nil {
			t.Fatal(err)
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		before, err := root.Lstat("body")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(body); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(body, 0600); err != nil {
			t.Fatal(err)
		}
		file, err := openChecked(root, "body", before)
		if file != nil {
			file.Close()
		}
		if !errors.Is(err, ErrUnsafe) || file != nil {
			t.Fatal("FIFO replacement was accepted", err)
		}
		return
	}
	// Probe filesystem support before running a bounded subprocess. A blocking
	// open cannot be interrupted safely by cancelling a goroutine's context.
	if err := unix.Mkfifo(filepath.Join(t.TempDir(), "probe"), 0600); err != nil {
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
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestOpenRejectsFIFOReplacementWithoutBlocking$")
	cmd.Env = append(os.Environ(), "GOSX_TEST_REGULAR_FIFO=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("FIFO replacement blocked without a writer")
	}
	if err != nil {
		t.Fatalf("FIFO replacement rejection failed: %s: %v", out, err)
	}
}

func TestOpenRejectsDevice(t *testing.T) {
	root, err := os.OpenRoot("/dev")
	if err != nil {
		t.Skip("device directory unavailable")
	}
	defer root.Close()
	info, err := root.Lstat("null")
	if err != nil || info.Mode()&os.ModeDevice == 0 {
		t.Skip("null device unavailable")
	}
	file, err := Open(root, "null")
	if file != nil {
		file.Close()
	}
	if !errors.Is(err, ErrUnsafe) || file != nil {
		t.Fatal("device was accepted", err)
	}
}
