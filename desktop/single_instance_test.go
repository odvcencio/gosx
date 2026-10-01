package desktop

import "testing"

func TestSingleInstanceMutexNameUsesSessionNamespace(t *testing.T) {
	if got, want := singleInstanceMutexName("com.example.app"), `Local\gosx-com.example.app`; got != want {
		t.Fatalf("singleInstanceMutexName() = %q, want %q", got, want)
	}
}

func TestInstanceLockRegistryLifecycle(t *testing.T) {
	const appID = "com.example.registry-test"
	if instanceLockHeld(appID) {
		t.Fatal("lock unexpectedly held before acquisition")
	}

	closeCalls := 0
	lock := newInstanceLock(appID, func() error {
		closeCalls++
		return nil
	})
	if !instanceLockHeld(appID) {
		t.Fatal("new lock is not registered")
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if instanceLockHeld(appID) {
		t.Fatal("lock remains registered after Close")
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if closeCalls != 1 {
		t.Fatalf("close callback called %d times, want 1", closeCalls)
	}
}
