package desktop

import (
	"sync"
	"unsafe"
)

// comGUID has the in-memory field order used by a Windows GUID. Keeping the
// value type platform-neutral lets Linux tests check the IIDs used by the
// syscall-based COM handlers.
type comGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var iidIUnknown = comGUID{
	Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
}

// WebView2 handler IIDs from the Microsoft.Web.WebView2 1.0.4191.47 SDK.
var (
	iidCreateCoreWebView2EnvironmentCompletedHandler = comGUID{
		Data1: 0x4e8a3389, Data2: 0xc9d8, Data3: 0x4bd2,
		Data4: [8]byte{0xb6, 0xb5, 0x12, 0x4f, 0xee, 0x6c, 0xc1, 0x4d},
	}
	iidCreateCoreWebView2ControllerCompletedHandler = comGUID{
		Data1: 0x6c4819f3, Data2: 0xc9b7, Data3: 0x4260,
		Data4: [8]byte{0x81, 0x27, 0xc9, 0xf5, 0xbd, 0xe7, 0xf6, 0x8c},
	}
	iidWebMessageReceivedEventHandler = comGUID{
		Data1: 0x57213f19, Data2: 0x00e6, Data3: 0x49fa,
		Data4: [8]byte{0x8e, 0x07, 0x89, 0x8e, 0xa0, 0x1e, 0xcb, 0xd2},
	}
	iidWebResourceRequestedEventHandler = comGUID{
		Data1: 0xab00b74c, Data2: 0x15f1, Data3: 0x4646,
		Data4: [8]byte{0x80, 0xe8, 0xe7, 0x63, 0x41, 0xd2, 0x5d, 0x71},
	}
	iidProcessFailedEventHandler = comGUID{
		Data1: 0x79e0aea4, Data2: 0x990b, Data3: 0x42d9,
		Data4: [8]byte{0xaa, 0x1d, 0x0f, 0xcc, 0x2e, 0x5b, 0xc7, 0xf1},
	}
	iidContainsFullScreenElementChangedEventHandler = comGUID{
		Data1: 0xe45d98b1, Data2: 0xafef, Data3: 0x45be,
		Data4: [8]byte{0x8b, 0xaf, 0x6c, 0x77, 0x28, 0x86, 0x7f, 0x73},
	}
	iidPermissionRequestedEventHandler = comGUID{
		Data1: 0x15e1c6a3, Data2: 0xc72a, Data3: 0x4df3,
		Data4: [8]byte{0x91, 0xd7, 0xd0, 0x97, 0xfb, 0xec, 0x6b, 0xfd},
	}
	iidNavigationCompletedEventHandler = comGUID{
		Data1: 0xd33a35bf, Data2: 0x1c49, Data3: 0x4f98,
		Data4: [8]byte{0x93, 0xab, 0x00, 0x6e, 0x05, 0x33, 0xfe, 0x1c},
	}
	// iidCoreWebView2Controller2 is queried, not implemented, so it is not in
	// the handler IID tests.
	iidCoreWebView2Controller2 = comGUID{
		Data1: 0xc979903e, Data2: 0xd4ca, Data3: 0x4228,
		Data4: [8]byte{0x92, 0xeb, 0x47, 0xee, 0x3f, 0xa9, 0x6e, 0xab},
	}
)

func supportsCOMInterface(requested, handlerIID comGUID) bool {
	return requested == iidIUnknown || requested == handlerIID
}

// comReference tracks one reference owned by Go. retainCOMReference first
// acquires a reference from a borrowed COM pointer; ownCOMReference tracks a
// pointer returned with an already-owned reference. Release is safe to call
// from both failure cleanup and normal teardown because it runs once.
type comReference struct {
	value   unsafe.Pointer
	release func(unsafe.Pointer)
	once    sync.Once
}

func retainCOMReference(value unsafe.Pointer, addRef, release func(unsafe.Pointer)) *comReference {
	if value == nil {
		return nil
	}
	if addRef != nil {
		addRef(value)
	}
	return ownCOMReference(value, release)
}

func ownCOMReference(value unsafe.Pointer, release func(unsafe.Pointer)) *comReference {
	if value == nil {
		return nil
	}
	return &comReference{value: value, release: release}
}

func (r *comReference) Release() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if r.release != nil {
			r.release(r.value)
		}
		r.value = nil
	})
}
