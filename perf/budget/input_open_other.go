//go:build !unix

package budget

import "os"

func openInput(root *os.Root, path string) (*os.File, error) {
	return root.Open(path)
}
