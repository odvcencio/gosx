//go:build unix

package budget

import (
	"os"
	"syscall"
)

// Nonblocking open lets the descriptor type check reject a FIFO before any
// read. Root still confines every path component and symlink during the open.
func openInput(root *os.Root, path string) (*os.File, error) {
	return root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
