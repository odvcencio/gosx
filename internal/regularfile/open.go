// Package regularfile confines reads to a stable regular file.
package regularfile

import (
	"errors"
	"os"
)

// ErrUnsafe means a path or opened descriptor is not an admitted regular file.
var ErrUnsafe = errors.New("unsafe regular file")

// Open checks the path before opening and the descriptor's type and identity
// before any read. SameFile compares the device and inode on Unix.
func Open(root *os.Root, name string) (*os.File, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, ErrUnsafe
	}
	return openChecked(root, name, before)
}

func openChecked(root *os.Root, name string, before os.FileInfo) (*os.File, error) {
	file, err := openRead(root, name)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		file.Close()
		return nil, ErrUnsafe
	}
	return file, nil
}
