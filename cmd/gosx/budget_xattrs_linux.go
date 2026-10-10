package main

import (
	"bytes"
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// Descriptor-based copies include Linux POSIX ACLs (system.posix_acl_access).
// Any unreadable or uncopyable attribute fails before replacing the source.
func copyBudgetExtendedAttributes(source, staged *os.File) error {
	original, err := budgetExtendedAttributes(source)
	if err != nil {
		return err
	}
	existing, err := budgetExtendedAttributes(staged)
	if err != nil {
		return err
	}
	for name := range existing {
		if _, keep := original[name]; !keep {
			if err := unix.Fremovexattr(int(staged.Fd()), name); err != nil {
				return err
			}
		}
	}
	for name, value := range original {
		if previous, exists := existing[name]; exists && bytes.Equal(previous, value) {
			continue
		}
		if err := unix.Fsetxattr(int(staged.Fd()), name, value, 0); err != nil {
			return err
		}
	}
	copied, err := budgetExtendedAttributes(staged)
	if err != nil {
		return err
	}
	if len(copied) != len(original) {
		return errors.New("output attributes changed")
	}
	for name, value := range original {
		if got, exists := copied[name]; !exists || !bytes.Equal(got, value) {
			return errors.New("output attributes changed")
		}
	}
	return nil
}

func budgetExtendedAttributes(file *os.File) (map[string][]byte, error) {
	// Linux limits both attribute values and the name list to 64 KiB.
	names := make([]byte, 64<<10)
	n, err := unix.Flistxattr(int(file.Fd()), names)
	if errors.Is(err, unix.ENOTSUP) {
		return map[string][]byte{}, nil
	}
	if err != nil {
		return nil, err
	}
	values := make(map[string][]byte)
	for _, name := range strings.Split(string(names[:n]), "\x00") {
		if name == "" {
			continue
		}
		value := make([]byte, 64<<10)
		n, err := unix.Fgetxattr(int(file.Fd()), name, value)
		if err != nil {
			return nil, err
		}
		values[name] = bytes.Clone(value[:n])
	}
	return values, nil
}
