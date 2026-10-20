package desktop

import "sync"

// InstanceLock keeps a Windows app ID reserved for the current process.
// Acquire it before doing startup work, and close it when the app exits.
type InstanceLock struct {
	appID    string
	closeFn  func() error
	once     sync.Once
	closeErr error
}

var heldInstanceLocks = struct {
	sync.Mutex
	byAppID map[string]*InstanceLock
}{byAppID: make(map[string]*InstanceLock)}

func newInstanceLock(appID string, closeFn func() error) *InstanceLock {
	lock := &InstanceLock{appID: appID, closeFn: closeFn}
	heldInstanceLocks.Lock()
	heldInstanceLocks.byAppID[appID] = lock
	heldInstanceLocks.Unlock()
	return lock
}

func instanceLockHeld(appID string) bool {
	heldInstanceLocks.Lock()
	defer heldInstanceLocks.Unlock()
	return heldInstanceLocks.byAppID[appID] != nil
}

// Close releases the lock. Closing a nil lock is a no-op.
func (lock *InstanceLock) Close() error {
	if lock == nil {
		return nil
	}
	lock.once.Do(func() {
		if lock.closeFn != nil {
			lock.closeErr = lock.closeFn()
		}
		heldInstanceLocks.Lock()
		if heldInstanceLocks.byAppID[lock.appID] == lock {
			delete(heldInstanceLocks.byAppID, lock.appID)
		}
		heldInstanceLocks.Unlock()
	})
	return lock.closeErr
}

func singleInstanceMutexName(appID string) string {
	return `Local\gosx-` + appID
}
