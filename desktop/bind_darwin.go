//go:build darwin && !ios && (amd64 || arm64)

package desktop

// This file is the whole of the darwin "FFI layer" for AppKit and WebKit: the
// load-time imports, the runtime gateways that call them, and a few thin typed
// wrappers. It copies the mechanism Tymbal's Core Audio backend uses
// (m31labs.dev/tymbal/internal/coreaudio, bind_darwin.go) and the standard
// library's crypto/x509/internal/macos uses for CoreFoundation and Security:
//
//   - A //go:cgo_import_dynamic directive per symbol makes the internal linker
//     record the framework once (an LC_LOAD_DYLIB) and have dyld bind the
//     symbol when the process starts. A cross-compile from Linux needs no SDK.
//   - A bare "JMP symbol" stub per import (bind_darwin.s) gives Go a function
//     address to call. The fn* variables publish those addresses from assembly,
//     because only the standard library may use abi.FuncPCABI0.
//   - //go:linkname gateways to syscall.syscall, syscall.syscall6 and
//     syscall.syscall9 (kept for x/sys, go.dev/issue/67401) call that address
//     on the system stack with the C ABI and bracket the call with
//     entersyscall/exitsyscall. They need neither runtime.cgocall nor iscgo.
//
// Everything else in the Objective-C world is reached by class and selector
// through objc_msgSend (objc_darwin.go), so a framework contributes at most one
// anchor import here. See hypha://m31labs/gosx/initiatives/2026-10-17-darwin-desktop-backend.
//
// Only the four-field form of the directive is accepted outside the standard
// library and cgo output, so //go:cgo_ldflag is unavailable and external
// linking is not supported. CGO_ENABLED=0 never uses it. A program that enables
// cgo for other reasons and links externally must pass
// CGO_LDFLAGS="-framework AppKit -framework WebKit -framework CoreFoundation -lobjc".

import (
	"syscall"
	"unsafe"
)

// Load-time imports. The first field is the symbol's link-time name, unique to
// this package; the second is the C name; the third is the install name. A
// missing symbol fails the whole binary at launch, so each name is long-standing
// public API.

// libobjc
//go:cgo_import_dynamic gosx_objc_msgSend objc_msgSend "/usr/lib/libobjc.A.dylib"
//go:cgo_import_dynamic gosx_objc_getClass objc_getClass "/usr/lib/libobjc.A.dylib"
//go:cgo_import_dynamic gosx_sel_registerName sel_registerName "/usr/lib/libobjc.A.dylib"
//go:cgo_import_dynamic gosx_objc_allocateClassPair objc_allocateClassPair "/usr/lib/libobjc.A.dylib"
//go:cgo_import_dynamic gosx_objc_registerClassPair objc_registerClassPair "/usr/lib/libobjc.A.dylib"
//go:cgo_import_dynamic gosx_class_addMethod class_addMethod "/usr/lib/libobjc.A.dylib"
//go:cgo_import_dynamic gosx_class_addIvar class_addIvar "/usr/lib/libobjc.A.dylib"
//go:cgo_import_dynamic gosx_class_getInstanceVariable class_getInstanceVariable "/usr/lib/libobjc.A.dylib"
//go:cgo_import_dynamic gosx_ivar_getOffset ivar_getOffset "/usr/lib/libobjc.A.dylib"
//go:cgo_import_dynamic gosx_object_setInstanceVariable object_setInstanceVariable "/usr/lib/libobjc.A.dylib"
//go:cgo_import_dynamic gosx_objc_retain objc_retain "/usr/lib/libobjc.A.dylib"
//go:cgo_import_dynamic gosx_objc_release objc_release "/usr/lib/libobjc.A.dylib"
//go:cgo_import_dynamic gosx_objc_autoreleasePoolPush objc_autoreleasePoolPush "/usr/lib/libobjc.A.dylib"
//go:cgo_import_dynamic gosx_objc_autoreleasePoolPop objc_autoreleasePoolPop "/usr/lib/libobjc.A.dylib"

// AppKit: one anchor so the linker emits the LC_LOAD_DYLIB and dyld loads the
// framework before objc_getClass("NSApplication") runs. Everything else in
// AppKit is reached by class and selector.
//go:cgo_import_dynamic gosx_NSApplicationLoad NSApplicationLoad "/System/Library/Frameworks/AppKit.framework/Versions/C/AppKit"

// WebKit: the same, from the exported C API.
//go:cgo_import_dynamic gosx_WKStringCreateWithUTF8CString WKStringCreateWithUTF8CString "/System/Library/Frameworks/WebKit.framework/Versions/A/WebKit"

// CoreFoundation: NSString is toll-free bridged to CFString, which is how
// strings come back out (cfStringToString).
//go:cgo_import_dynamic gosx_CFStringGetLength CFStringGetLength "/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation"
//go:cgo_import_dynamic gosx_CFStringGetMaximumSizeForEncoding CFStringGetMaximumSizeForEncoding "/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation"
//go:cgo_import_dynamic gosx_CFStringGetCString CFStringGetCString "/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation"

// libSystem
//go:cgo_import_dynamic gosx_pthread_main_np pthread_main_np "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic gosx_pthread_sigmask pthread_sigmask "/usr/lib/libSystem.B.dylib"

// fn* hold the address of each import's trampoline. bind_darwin.s defines them
// with DATA directives; Go never assigns them.
var (
	fnobjc_msgSend                      uintptr
	fnobjc_getClass                     uintptr
	fnsel_registerName                  uintptr
	fnobjc_allocateClassPair            uintptr
	fnobjc_registerClassPair            uintptr
	fnclass_addMethod                   uintptr
	fnclass_addIvar                     uintptr
	fnclass_getInstanceVariable         uintptr
	fnivar_getOffset                    uintptr
	fnobject_setInstanceVariable        uintptr
	fnobjc_retain                       uintptr
	fnobjc_release                      uintptr
	fnobjc_autoreleasePoolPush          uintptr
	fnobjc_autoreleasePoolPop           uintptr
	fnNSApplicationLoad                 uintptr
	fnWKStringCreateWithUTF8CString     uintptr
	fnCFStringGetLength                 uintptr
	fnCFStringGetMaximumSizeForEncoding uintptr
	fnCFStringGetCString                uintptr
	fnpthread_main_np                   uintptr
	fnpthread_sigmask                   uintptr
)

// Runtime gateways. These are the pull side of the linknames that the Go
// runtime and package syscall keep alive for golang.org/x/sys/unix ("Do not
// remove or change the type signature", go.dev/issue/67401). Each one switches
// to the thread's system stack, calls fn with the C ABI and brackets the call
// with entersyscall/exitsyscall, so a blocking C call (the event pump) does not
// stall the scheduler.
//
// The declarations have no body, so the compiler keeps any pointer passed as
// uintptr(unsafe.Pointer(p)) in the argument list alive for the whole call. Go
// memory may therefore be passed to a call that does not retain it. Memory that
// Objective-C keeps after the call returns lives in the native arena
// (ctrl_darwin.go) instead.

//go:noescape
func syscall_syscall(fn, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno)

//go:noescape
func syscall_syscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)

//go:noescape
func syscall_syscall9(fn, a1, a2, a3, a4, a5, a6, a7, a8, a9 uintptr) (r1, r2 uintptr, err syscall.Errno)

//go:linkname syscall_syscall syscall.syscall
//go:linkname syscall_syscall6 syscall.syscall6
//go:linkname syscall_syscall9 syscall.syscall9

// frameworksLinked reports whether every anchor import has a published
// trampoline. It also keeps the AppKit and WebKit anchors reachable, which is
// what makes the linker emit their LC_LOAD_DYLIB commands: dyld then loads
// both frameworks at launch, before objc_getClass("NSApplication") runs.
func frameworksLinked() bool {
	return fnNSApplicationLoad != 0 && fnWKStringCreateWithUTF8CString != 0
}

const kCFStringEncodingUTF8 = 0x08000100

// cfStringToString copies a CFStringRef (an NSString) into a Go string. It does
// not release ref.
func cfStringToString(ref uintptr) (string, bool) {
	if ref == 0 {
		return "", false
	}
	length, _, _ := syscall_syscall(fnCFStringGetLength, ref, 0, 0)
	// CFIndex CFStringGetMaximumSizeForEncoding(CFIndex length, CFStringEncoding): -1 when unrepresentable.
	max, _, _ := syscall_syscall(fnCFStringGetMaximumSizeForEncoding, length, kCFStringEncodingUTF8, 0)
	if int(max) < 0 || int(max) > 1<<28 {
		return "", false
	}
	buf := make([]byte, int(max)+1)
	// Boolean CFStringGetCString(CFStringRef, char *buffer, CFIndex bufferSize, CFStringEncoding).
	ok, _, _ := syscall_syscall6(fnCFStringGetCString, ref, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
		kCFStringEncodingUTF8, 0, 0)
	if ok&0xff == 0 {
		return "", false
	}
	for i, c := range buf {
		if c == 0 {
			return string(buf[:i]), true
		}
	}
	return "", false
}

// pthreadMainNP is int pthread_main_np(void): 1 on the process's main thread.
func pthreadMainNP() int {
	r, _, _ := syscall_syscall(fnpthread_main_np, 0, 0, 0)
	return int(int32(r))
}

// pthreadSigmask is int pthread_sigmask(int how, const sigset_t *set, sigset_t *oldset).
// A sigset_t is one uint32 on macOS, with bit sig-1 standing for signal sig.
func pthreadSigmask(how int, set, old *uint32) int32 {
	r, _, _ := syscall_syscall(fnpthread_sigmask, uintptr(how), uintptr(unsafe.Pointer(set)), uintptr(unsafe.Pointer(old)))
	return int32(r)
}

// objcAutoreleasePoolPush is void *objc_autoreleasePoolPush(void). The pool is
// per thread: pop it on the thread that pushed it.
func objcAutoreleasePoolPush() uintptr {
	r, _, _ := syscall_syscall(fnobjc_autoreleasePoolPush, 0, 0, 0)
	return r
}

// objcAutoreleasePoolPop is void objc_autoreleasePoolPop(void *pool).
func objcAutoreleasePoolPop(pool uintptr) {
	syscall_syscall(fnobjc_autoreleasePoolPop, pool, 0, 0)
}

// objcRetain is id objc_retain(id).
func objcRetain(obj uintptr) uintptr {
	r, _, _ := syscall_syscall(fnobjc_retain, obj, 0, 0)
	return r
}

// objcRelease is void objc_release(id).
func objcRelease(obj uintptr) {
	if obj != 0 {
		syscall_syscall(fnobjc_release, obj, 0, 0)
	}
}
