//go:build darwin && !ios && arm64

#include "textflag.h"

// Argument shims for objc_msgSend, arm64.
//
// syscall.syscall* loads integer registers only. AppKit methods that take a
// CGFloat structure by value want floating-point registers instead: under
// AAPCS64 (Apple's variant) an NSRect is a homogeneous floating-point
// aggregate of four doubles and travels in V0-V3, an NSSize in V0-V1, and a
// bare CGFloat in V0. A shim is a NOSPLIT|NOFRAME function that
// syscall.syscall* calls as its fn: it loads the floating-point registers from
// memory, moves the remaining integer arguments into the places the method
// wants them, and tail-jumps to objc_msgSend, which then dispatches as if the
// original caller had set the registers itself.
//
// A shim runs on the system stack, called by the runtime's syscallN trampoline
// with the C ABI. It touches only R0-R5 and V0-V3 (all caller-saved), refers to
// no symbol except the branch target (a direct branch, which does not use the
// assembler temporary R27), and does not return: objc_msgSend returns to the
// runtime's trampoline.
//
// The addresses of the shims reach Go through fnMsgSend* (see objc_darwin.go).

// id msgSendRect(id self, SEL sel, const NSRect *rect, a, b, c)
//
// Calls objc_msgSend(self, sel, *rect, a, b, c) where the method's signature is
// (NSRect, NSInteger-or-pointer, NSInteger-or-pointer, NSInteger-or-pointer).
// Used by initWithContentRect:styleMask:backing:defer:, initWithFrame:configuration:
// and valueWithRect:.
TEXT ·msgSendRect(SB), NOSPLIT|NOFRAME, $0-0
	FMOVD	0(R2), F0
	FMOVD	8(R2), F1
	FMOVD	16(R2), F2
	FMOVD	24(R2), F3
	MOVD	R3, R2
	MOVD	R4, R3
	MOVD	R5, R4
	B	gosx_objc_msgSend(SB)

// id msgSend4F(id self, SEL sel, const double *f)
//
// Calls objc_msgSend(self, sel, f[0], f[1], f[2], f[3]) where all four are
// CGFloat arguments, which AAPCS64 passes in V0-V3. A method with fewer
// floating-point arguments (dateWithTimeIntervalSinceNow: takes one) ignores the
// extra registers. Used by colorWithSRGBRed:green:blue:alpha:.
TEXT ·msgSend4F(SB), NOSPLIT|NOFRAME, $0-0
	FMOVD	0(R2), F0
	FMOVD	8(R2), F1
	FMOVD	16(R2), F2
	FMOVD	24(R2), F3
	B	gosx_objc_msgSend(SB)

// id msgSendZeroFP3(id self, SEL sel, a2, a3, a4, a5, a6, a7, stack0)
//
// Calls objc_msgSend with V0-V2 cleared. It is for a method that takes a
// zero-valued NSPoint and NSTimeInterval ahead of integer arguments, namely
// +[NSEvent otherEventWithType:location:modifierFlags:timestamp:windowNumber:
// context:subtype:data1:data2:], whose location (V0, V1) and timestamp (V2) are
// zero here. The integer arguments are already where Go put them: the runtime's
// syscallN trampoline fills R0-R7 and pushes the ninth argument at 0(SP).
TEXT ·msgSendZeroFP3(SB), NOSPLIT|NOFRAME, $0-0
	FMOVD	ZR, F0
	FMOVD	ZR, F1
	FMOVD	ZR, F2
	B	gosx_objc_msgSend(SB)

GLOBL	·fnMsgSendRect(SB), NOPTR|RODATA, $8
DATA	·fnMsgSendRect(SB)/8, $·msgSendRect(SB)
GLOBL	·fnMsgSend4F(SB), NOPTR|RODATA, $8
DATA	·fnMsgSend4F(SB)/8, $·msgSend4F(SB)
GLOBL	·fnMsgSendZeroFP3(SB), NOPTR|RODATA, $8
DATA	·fnMsgSendZeroFP3(SB)/8, $·msgSendZeroFP3(SB)
