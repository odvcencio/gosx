//go:build darwin && !ios && (amd64 || arm64)

#include "textflag.h"

// One trampoline per imported symbol, as in crypto/x509/internal/macos
// (corefoundation.s), golang.org/x/sys/unix (zsyscall_darwin_*.s) and Tymbal's
// internal/coreaudio (bind_darwin.s).
//
// The body is a bare jump to the dynamically imported symbol (see the
// //go:cgo_import_dynamic lines in bind_darwin.go); the linker routes it through
// its own stub and binding slot. Because it is a jump and not a call, the C
// function sees the caller's arguments and returns to the caller, so the
// trampoline's address is itself a valid C function pointer. Go passes it to
// syscall.syscall for calls made from Go, and the delegate recorder calls
// objc_retain through the pointer stored in the control block.
//
// The fn* DATA symbols publish each trampoline's address to Go.
//
// The argument shims in msgsend_darwin_*.s jump to gosx_objc_msgSend directly
// instead of through a trampoline; both reach the same linker stub.

TEXT gosx_objc_msgSend_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_objc_msgSend(SB)
GLOBL	·fnobjc_msgSend(SB), RODATA, $8
DATA	·fnobjc_msgSend(SB)/8, $gosx_objc_msgSend_trampoline<>(SB)

TEXT gosx_objc_getClass_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_objc_getClass(SB)
GLOBL	·fnobjc_getClass(SB), RODATA, $8
DATA	·fnobjc_getClass(SB)/8, $gosx_objc_getClass_trampoline<>(SB)

TEXT gosx_sel_registerName_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_sel_registerName(SB)
GLOBL	·fnsel_registerName(SB), RODATA, $8
DATA	·fnsel_registerName(SB)/8, $gosx_sel_registerName_trampoline<>(SB)

TEXT gosx_objc_allocateClassPair_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_objc_allocateClassPair(SB)
GLOBL	·fnobjc_allocateClassPair(SB), RODATA, $8
DATA	·fnobjc_allocateClassPair(SB)/8, $gosx_objc_allocateClassPair_trampoline<>(SB)

TEXT gosx_objc_registerClassPair_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_objc_registerClassPair(SB)
GLOBL	·fnobjc_registerClassPair(SB), RODATA, $8
DATA	·fnobjc_registerClassPair(SB)/8, $gosx_objc_registerClassPair_trampoline<>(SB)

TEXT gosx_class_addMethod_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_class_addMethod(SB)
GLOBL	·fnclass_addMethod(SB), RODATA, $8
DATA	·fnclass_addMethod(SB)/8, $gosx_class_addMethod_trampoline<>(SB)

TEXT gosx_class_addIvar_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_class_addIvar(SB)
GLOBL	·fnclass_addIvar(SB), RODATA, $8
DATA	·fnclass_addIvar(SB)/8, $gosx_class_addIvar_trampoline<>(SB)

TEXT gosx_class_getInstanceVariable_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_class_getInstanceVariable(SB)
GLOBL	·fnclass_getInstanceVariable(SB), RODATA, $8
DATA	·fnclass_getInstanceVariable(SB)/8, $gosx_class_getInstanceVariable_trampoline<>(SB)

TEXT gosx_ivar_getOffset_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_ivar_getOffset(SB)
GLOBL	·fnivar_getOffset(SB), RODATA, $8
DATA	·fnivar_getOffset(SB)/8, $gosx_ivar_getOffset_trampoline<>(SB)

TEXT gosx_object_setInstanceVariable_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_object_setInstanceVariable(SB)
GLOBL	·fnobject_setInstanceVariable(SB), RODATA, $8
DATA	·fnobject_setInstanceVariable(SB)/8, $gosx_object_setInstanceVariable_trampoline<>(SB)

TEXT gosx_objc_retain_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_objc_retain(SB)
GLOBL	·fnobjc_retain(SB), RODATA, $8
DATA	·fnobjc_retain(SB)/8, $gosx_objc_retain_trampoline<>(SB)

TEXT gosx_objc_release_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_objc_release(SB)
GLOBL	·fnobjc_release(SB), RODATA, $8
DATA	·fnobjc_release(SB)/8, $gosx_objc_release_trampoline<>(SB)

TEXT gosx_objc_autoreleasePoolPush_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_objc_autoreleasePoolPush(SB)
GLOBL	·fnobjc_autoreleasePoolPush(SB), RODATA, $8
DATA	·fnobjc_autoreleasePoolPush(SB)/8, $gosx_objc_autoreleasePoolPush_trampoline<>(SB)

TEXT gosx_objc_autoreleasePoolPop_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_objc_autoreleasePoolPop(SB)
GLOBL	·fnobjc_autoreleasePoolPop(SB), RODATA, $8
DATA	·fnobjc_autoreleasePoolPop(SB)/8, $gosx_objc_autoreleasePoolPop_trampoline<>(SB)

TEXT gosx_NSApplicationLoad_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_NSApplicationLoad(SB)
GLOBL	·fnNSApplicationLoad(SB), RODATA, $8
DATA	·fnNSApplicationLoad(SB)/8, $gosx_NSApplicationLoad_trampoline<>(SB)

TEXT gosx_WKStringCreateWithUTF8CString_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_WKStringCreateWithUTF8CString(SB)
GLOBL	·fnWKStringCreateWithUTF8CString(SB), RODATA, $8
DATA	·fnWKStringCreateWithUTF8CString(SB)/8, $gosx_WKStringCreateWithUTF8CString_trampoline<>(SB)

TEXT gosx_CFStringGetLength_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_CFStringGetLength(SB)
GLOBL	·fnCFStringGetLength(SB), RODATA, $8
DATA	·fnCFStringGetLength(SB)/8, $gosx_CFStringGetLength_trampoline<>(SB)

TEXT gosx_CFStringGetMaximumSizeForEncoding_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_CFStringGetMaximumSizeForEncoding(SB)
GLOBL	·fnCFStringGetMaximumSizeForEncoding(SB), RODATA, $8
DATA	·fnCFStringGetMaximumSizeForEncoding(SB)/8, $gosx_CFStringGetMaximumSizeForEncoding_trampoline<>(SB)

TEXT gosx_CFStringGetCString_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_CFStringGetCString(SB)
GLOBL	·fnCFStringGetCString(SB), RODATA, $8
DATA	·fnCFStringGetCString(SB)/8, $gosx_CFStringGetCString_trampoline<>(SB)

TEXT gosx_pthread_main_np_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_pthread_main_np(SB)
GLOBL	·fnpthread_main_np(SB), RODATA, $8
DATA	·fnpthread_main_np(SB)/8, $gosx_pthread_main_np_trampoline<>(SB)

TEXT gosx_pthread_sigmask_trampoline<>(SB),NOSPLIT,$0-0
	JMP	gosx_pthread_sigmask(SB)
GLOBL	·fnpthread_sigmask(SB), RODATA, $8
DATA	·fnpthread_sigmask(SB)/8, $gosx_pthread_sigmask_trampoline<>(SB)
