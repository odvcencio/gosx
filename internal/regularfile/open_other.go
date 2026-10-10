//go:build !unix

package regularfile

import "os"

func openRead(root *os.Root, name string) (*os.File, error) {
	return root.Open(name)
}
