//go:build darwin && !ios && (amd64 || arm64)

package desktop

// The Objective-C layer: class and selector caches, typed objc_msgSend helpers,
// NSString in and out, autorelease pools, and registration of the delegate class
// whose methods are assembly (imp_darwin_*.s).
//
// Calls out of Go are objc_msgSend through syscall.syscall, syscall6 or
// syscall9 (bind_darwin.go). Those carry integer registers only, which covers
// every BOOL, NSInteger, id, SEL and pointer argument. The few methods that take
// a CGFloat structure go through an argument shim (msgsend_darwin_*.s): see
// sendRect, sendFloats, sendSize and postWakeEvent. Methods that return a
// floating-point value or a structure (frame, bounds, backingScaleFactor) are not
// used in DT0; the return shims are DT2.
//
// Every object these helpers return that Go did not alloc itself is autoreleased
// and lives until the enclosing pool pops. The main goroutine pushes a pool per
// event-loop iteration; a helper called anywhere else brackets its own pool, on
// a goroutine locked to its thread, because a pool belongs to the thread that
// pushed it.

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

// Argument shims (msgsend_darwin_{arm64,amd64}.s). The functions are never
// called from Go; they are declared so that go vet can check the assembly's
// frame against them, and their C-ABI entry addresses are published through the
// fnMsgSend* variables, because the func value of an assembly function points
// at an ABIInternal wrapper rather than at the entry.
func msgSendRect()
func msgSend4F()
func msgSendZeroFP3()

var (
	fnMsgSendRect    uintptr
	fnMsgSend4F      uintptr
	fnMsgSendZeroFP3 uintptr
)

// Constants from the AppKit and WebKit headers.
const (
	nsWindowStyleTitled         = 1 << 0
	nsWindowStyleClosable       = 1 << 1
	nsWindowStyleMiniaturizable = 1 << 2
	nsWindowStyleResizable      = 1 << 3
	nsWindowStyleDefault        = nsWindowStyleTitled | nsWindowStyleClosable | nsWindowStyleMiniaturizable | nsWindowStyleResizable

	nsBackingStoreBuffered = 2

	nsApplicationActivationPolicyRegular    = 0
	nsApplicationActivationPolicyAccessory  = 1
	nsApplicationActivationPolicyProhibited = 2

	nsEventTypeApplicationDefined = 15
	nsEventMaskAny                = ^uintptr(0)

	// NSEventModifierFlags, as an NSMenuItem's keyEquivalentModifierMask.
	nsEventModifierFlagShift   = 1 << 17
	nsEventModifierFlagControl = 1 << 18
	nsEventModifierFlagOption  = 1 << 19
	nsEventModifierFlagCommand = 1 << 20

	nsControlStateValueOn = 1

	// NSAlertStyle, and the NSModalResponse of the first button (the others
	// follow it).
	nsAlertStyleWarning       = 0
	nsAlertStyleInformational = 1
	nsAlertStyleCritical      = 2
	nsAlertFirstButtonReturn  = 1000

	nsViewWidthSizable  = 2
	nsViewHeightSizable = 16

	wkUserScriptInjectionTimeAtDocumentStart = 0
)

// nsRect is NSRect on a 64-bit Mac: {{x, y}, {width, height}}, four doubles.
type nsRect struct{ x, y, w, h float64 }

// objcCString returns s as a NUL-terminated byte slice.
func objcCString(s string) []byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return b
}

var (
	objcMu     sync.RWMutex
	selCache   = map[string]uintptr{}
	classCache = map[string]uintptr{}
)

// objcSel is SEL sel_registerName(const char *): interned for the life of the
// process, so the result is cached.
func objcSel(name string) uintptr {
	objcMu.RLock()
	v, ok := selCache[name]
	objcMu.RUnlock()
	if ok {
		return v
	}
	b := objcCString(name)
	v, _, _ = syscall_syscall(fnsel_registerName, uintptr(unsafe.Pointer(&b[0])), 0, 0)
	objcMu.Lock()
	selCache[name] = v
	objcMu.Unlock()
	return v
}

// objcClass is Class objc_getClass(const char *): 0 when the class does not
// exist (a framework that is not loaded, or an API newer than the OS).
func objcClass(name string) uintptr {
	objcMu.RLock()
	v, ok := classCache[name]
	objcMu.RUnlock()
	if ok && v != 0 {
		return v
	}
	b := objcCString(name)
	v, _, _ = syscall_syscall(fnobjc_getClass, uintptr(unsafe.Pointer(&b[0])), 0, 0)
	if v != 0 {
		objcMu.Lock()
		classCache[name] = v
		objcMu.Unlock()
	}
	return v
}

// Typed objc_msgSend helpers. The number is the count of arguments after self
// and _cmd. The first three argument slots fit syscall; more go through
// syscall6 and syscall9. Unused slots carry zeros the callee never reads.

func send0(self, sel uintptr) uintptr {
	r, _, _ := syscall_syscall(fnobjc_msgSend, self, sel, 0)
	return r
}

func send1(self, sel, a uintptr) uintptr {
	r, _, _ := syscall_syscall(fnobjc_msgSend, self, sel, a)
	return r
}

func send2(self, sel, a, b uintptr) uintptr {
	r, _, _ := syscall_syscall6(fnobjc_msgSend, self, sel, a, b, 0, 0)
	return r
}

func send3(self, sel, a, b, c uintptr) uintptr {
	r, _, _ := syscall_syscall6(fnobjc_msgSend, self, sel, a, b, c, 0)
	return r
}

func send4(self, sel, a, b, c, d uintptr) uintptr {
	r, _, _ := syscall_syscall6(fnobjc_msgSend, self, sel, a, b, c, d)
	return r
}

// sendBool0 sends a message that returns BOOL. A BOOL result is the low byte of
// the return register.
func sendBool0(self, sel uintptr) bool { return send0(self, sel)&0xff != 0 }

// sendBool1 is sendBool0 with one argument.
func sendBool1(self, sel, a uintptr) bool { return send1(self, sel, a)&0xff != 0 }

// boolArg converts b to a BOOL argument.
func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// sendRect sends a message whose first argument after _cmd is an NSRect, followed
// by up to three integer-class arguments (msgSendRect).
func sendRect(self, sel uintptr, rect *nsRect, a, b, c uintptr) uintptr {
	r, _, _ := syscall_syscall6(fnMsgSendRect, self, sel, uintptr(unsafe.Pointer(rect)), a, b, c)
	return r
}

// sendFloats sends a message whose arguments are up to four CGFloats and
// nothing else (msgSend4F).
func sendFloats(self, sel uintptr, f *[4]float64) uintptr {
	r, _, _ := syscall_syscall(fnMsgSend4F, self, sel, uintptr(unsafe.Pointer(f)))
	return r
}

// sendSize sends a message whose only argument is an NSSize, which travels as two
// CGFloats in the floating-point registers (msgSend4F loads them).
func sendSize(self, sel uintptr, width, height float64) uintptr {
	f := [4]float64{width, height}
	return sendFloats(self, sel, &f)
}

// respondsTo is -[NSObject respondsToSelector:].
func respondsTo(obj uintptr, selector string) bool {
	return sendBool1(obj, objcSel("respondsToSelector:"), objcSel(selector))
}

// objcNew is [[cls alloc] init]: an owned object, +1, which the caller must
// release.
func objcNew(cls uintptr) uintptr {
	return send0(send0(cls, objcSel("alloc")), objcSel("init"))
}

// objcRef is one reference to an Objective-C object that Go owns. Release is
// safe to call from both failure cleanup and normal teardown because it runs
// once, the same shape as comReference for the Windows backend.
type objcRef struct {
	obj  uintptr
	once sync.Once
}

// ownObjC wraps an object returned with a reference Go already owns (alloc/init
// or objc_retain). It returns nil for a nil object.
func ownObjC(obj uintptr) *objcRef {
	if obj == 0 {
		return nil
	}
	return &objcRef{obj: obj}
}

// Release drops the reference. It is safe on a nil receiver.
func (r *objcRef) Release() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		objcRelease(r.obj)
		r.obj = 0
	})
}

// withPool runs fn inside an autorelease pool. A pool belongs to the thread that
// pushed it and popping it from another thread corrupts that thread's pool
// stack, so withPool pins the goroutine to its OS thread from push to pop; a
// goroutine that is not locked can otherwise move to another thread at any
// allocation or preemption point. On the main goroutine, which init already
// locked to the main thread, the extra lock is a counter increment.
func withPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objcAutoreleasePoolPush()
	defer objcAutoreleasePoolPop(pool)
	fn()
}

// nsString returns an autoreleased NSString holding s. A string cannot carry a
// NUL through stringWithUTF8String:, and the method returns nil for invalid
// UTF-8, so both are replaced with U+FFFD rather than dropping the argument.
func nsString(s string) uintptr {
	s = strings.ToValidUTF8(s, "�")
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "�")
	}
	b := objcCString(s)
	r, _, _ := syscall_syscall(fnobjc_msgSend, objcClass("NSString"), objcSel("stringWithUTF8String:"), uintptr(unsafe.Pointer(&b[0])))
	return r
}

// goString copies an NSString into a Go string. It does not release it.
func goString(ns uintptr) string {
	s, _ := cfStringToString(ns)
	return s
}

// nsURL returns an autoreleased NSURL for s, or 0 when s is not a valid URL.
func nsURL(s string) uintptr {
	return send1(objcClass("NSURL"), objcSel("URLWithString:"), nsString(s))
}

// nsNumberBool returns an autoreleased NSNumber holding b.
func nsNumberBool(b bool) uintptr {
	return send1(objcClass("NSNumber"), objcSel("numberWithBool:"), boolArg(b))
}

// nsColorSRGB returns an autoreleased sRGB NSColor.
func nsColorSRGB(r, g, b, a float64) uintptr {
	f := [4]float64{r, g, b, a}
	return sendFloats(objcClass("NSColor"), objcSel("colorWithSRGBRed:green:blue:alpha:"), &f)
}

// The delegate class. One instance serves as NSApplicationDelegate,
// NSWindowDelegate, WKNavigationDelegate and WKUIDelegate; AppKit and WebKit use
// respondsToSelector:, so protocol conformance need not be declared. The
// recorder finds its ctrl block through the one instance variable, at offset 8
// of a direct NSObject subclass (after isa). The offset is asserted when the
// class is registered, because the assembly hard-wires it.
const (
	delegateClassName  = "GoSXDelegate"
	delegateIvarName   = "ctrl"
	delegateIvarOffset = 8
)

var (
	delegateOnce sync.Once
	delegateCls  uintptr
	delegateErr  error
)

// delegateClass registers GoSXDelegate once per process and returns it.
func delegateClass() (uintptr, error) {
	delegateOnce.Do(func() { delegateCls, delegateErr = registerDelegateClass() })
	return delegateCls, delegateErr
}

// objcBoolEncoding is the type encoding of BOOL: a C bool ('B') on arm64, a
// signed char ('c') on amd64.
func objcBoolEncoding() string {
	if runtime.GOARCH == "amd64" {
		return "c"
	}
	return "B"
}

// recorderMethod is one selector of a recorder class and the assembly stub that
// implements it.
type recorderMethod struct {
	selector string
	stub     uintptr // C-ABI entry of the stub (an impStubs element)
	types    string  // Objective-C type encoding
}

// registerRecorderClass creates and registers an NSObject subclass called name
// whose instances carry the "ctrl" instance variable and whose methods are the
// given stubs. It asserts the instance variable's offset, because the recorder
// reads it at a fixed offset from self.
func registerRecorderClass(name string, methods []recorderMethod) (uintptr, error) {
	if !frameworksLinked() {
		return 0, fmt.Errorf("%w: AppKit or WebKit import was not linked", ErrUnsupported)
	}
	super := objcClass("NSObject")
	if super == 0 {
		return 0, fmt.Errorf("%w: libobjc has no NSObject", ErrUnsupported)
	}
	cname := objcCString(name)
	cls, _, _ := syscall_syscall(fnobjc_allocateClassPair, super, uintptr(unsafe.Pointer(&cname[0])), 0)
	if cls == 0 {
		return 0, fmt.Errorf("desktop: objc_allocateClassPair(%s) failed; the class name is already registered", name)
	}
	ivar := objcCString(delegateIvarName)
	ivarType := objcCString("^v")
	// BOOL class_addIvar(Class, const char *name, size_t size, uint8_t alignment, const char *types).
	if ok, _, _ := syscall_syscall6(fnclass_addIvar, cls, uintptr(unsafe.Pointer(&ivar[0])), 8, 3, uintptr(unsafe.Pointer(&ivarType[0])), 0); ok&0xff == 0 {
		return 0, fmt.Errorf("desktop: class_addIvar(%s.%s) failed", name, delegateIvarName)
	}
	for _, m := range methods {
		types := objcCString(m.types)
		// BOOL class_addMethod(Class, SEL, IMP, const char *types).
		ok, _, _ := syscall_syscall6(fnclass_addMethod, cls, objcSel(m.selector), m.stub, uintptr(unsafe.Pointer(&types[0])), 0, 0)
		if ok&0xff == 0 {
			return 0, fmt.Errorf("desktop: class_addMethod(%s %s) failed", name, m.selector)
		}
	}
	syscall_syscall(fnobjc_registerClassPair, cls, 0, 0)

	off, err := ivarOffsetOf(cls)
	if err != nil {
		return 0, err
	}
	if off != delegateIvarOffset {
		return 0, fmt.Errorf("desktop: %s.%s is at offset %d, but the delegate recorder reads offset %d", name, delegateIvarName, off, delegateIvarOffset)
	}
	return cls, nil
}

func registerDelegateClass() (uintptr, error) {
	boolEnc := objcBoolEncoding()
	return registerRecorderClass(delegateClassName, []recorderMethod{
		{"windowShouldClose:", impStubs[impStubWindowShouldClose], boolEnc + "@:@"},
		{"applicationShouldTerminate:", impStubs[impStubApplicationShouldTerminate], "Q@:@"},
		{"application:openFiles:", impStubs[impStubOpenFiles], "v@:@@"},
		{"application:openURLs:", impStubs[impStubOpenURLs], "v@:@@"},
		{"userContentController:didReceiveScriptMessage:", impStubs[impStubWebMessage], "v@:@@"},
		{"webView:runOpenPanelWithParameters:initiatedByFrame:completionHandler:", impStubs[impStubWebFilePanel], "v@:@@@@?"},
		{"webView:runJavaScriptAlertPanelWithMessage:initiatedByFrame:completionHandler:", impStubs[impStubWebAlert], "v@:@@@@?"},
		{"webView:runJavaScriptConfirmPanelWithMessage:initiatedByFrame:completionHandler:", impStubs[impStubWebConfirm], "v@:@@@@?"},
		{"webView:runJavaScriptTextInputPanelWithPrompt:defaultText:initiatedByFrame:completionHandler:", impStubs[impStubWebPrompt], "v@:@@@@@?"},
		{"webView:didFinishNavigation:", impStubs[impStubNavigationFinished], "v@:@@"},
		{"webView:didFailNavigation:withError:", impStubs[impStubNavigationFailed], "v@:@@@"},
		{"webView:didFailProvisionalNavigation:withError:", impStubs[impStubNavigationFailed], "v@:@@@"},
	})
}

// ivarOffsetOf returns ivar_getOffset(class_getInstanceVariable(cls, "ctrl")).
func ivarOffsetOf(cls uintptr) (int, error) {
	name := objcCString(delegateIvarName)
	ivar, _, _ := syscall_syscall(fnclass_getInstanceVariable, cls, uintptr(unsafe.Pointer(&name[0])), 0)
	if ivar == 0 {
		return 0, fmt.Errorf("desktop: class has no %s instance variable", delegateIvarName)
	}
	off, _, _ := syscall_syscall(fnivar_getOffset, ivar, 0, 0)
	return int(off), nil
}

// newDelegate returns an owned instance of the delegate class whose ctrl
// instance variable points at ring's control block.
func newDelegate(cls uintptr, ring *nativeRing) (uintptr, error) {
	return newRecorderObject(cls, ring)
}

// newRecorderObject returns an owned instance of a recorder class whose ctrl
// instance variable points at ring's control block.
func newRecorderObject(cls uintptr, ring *nativeRing) (uintptr, error) {
	obj := objcNew(cls)
	if obj == 0 {
		return 0, fmt.Errorf("desktop: allocate recorder object")
	}
	name := objcCString(delegateIvarName)
	// Ivar object_setInstanceVariable(id, const char *name, void *value): stores
	// the pointer itself in the instance variable.
	ivar, _, _ := syscall_syscall(fnobject_setInstanceVariable, obj, uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(ring.c)))
	if ivar == 0 {
		objcRelease(obj)
		return 0, fmt.Errorf("desktop: object_setInstanceVariable(%s) failed", delegateIvarName)
	}
	return obj, nil
}
