//go:build unix

package regularfile

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func openRead(root *os.Root, name string) (*os.File, error) {
	// Root.OpenFile resolves a final symlink even with O_NOFOLLOW. Confine the
	// parent first, then open its final component without symlink resolution.
	parent, err := root.OpenRoot(filepath.Dir(name))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	directory, err := parent.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	fd, err := unix.Openat(int(directory.Fd()), filepath.Base(name), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}
