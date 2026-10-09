package main

import (
	"os"

	"golang.org/x/sys/windows"
)

const budgetOpenFlags = 0

func acquireBudgetLock(file *os.File) error {
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &windows.Overlapped{})
}

func openBudgetDirectory(root *os.Root, path string) (*os.File, error) {
	dir, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	// FlushFileBuffers needs GENERIC_WRITE. Reopen the confined handle with
	// backup semantics, without resolving an unconfined filesystem path.
	reopen := windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")
	handle, _, err := reopen.Call(dir.Fd(), windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_FLAG_BACKUP_SEMANTICS)
	if windows.Handle(handle) == windows.InvalidHandle {
		return nil, err
	}
	return os.NewFile(handle, path), nil
}
