//go:build darwin && !ios && amd64

#include "textflag.h"

// Argument shims for objc_msgSend, amd64. See msgsend_darwin_arm64.s for why
// they exist; this file is the System V AMD64 version.
//
// Under the System V ABI an NSRect (32 bytes, four doubles) is class MEMORY, so
// the caller copies it onto the stack at the call site; an NSSize or NSPoint
// (two doubles) goes in two SSE registers; a bare CGFloat goes in XMM0. The
// integer arguments use DI, SI, DX, CX, R8, R9 in order, then the stack.
//
// A shim runs on the system stack, called by the runtime's syscallN trampoline
// with the C ABI. It touches only caller-saved registers (AX, CX, DX, R8, R9,
// X0-X3) and refers to no symbol except the call or jump target. The stack is
// 16-byte aligned at every call, as the C ABI requires: on entry SP is 8 mod
// 16 (the return address), so msgSendRect's 40-byte frame leaves it 0 mod 16.
// The frame is built with SUBQ and ADDQ rather than PUSHQ and POPQ because the
// assembler checks PUSH/POP balance in program order.
//
// The addresses of the shims reach Go through fnMsgSend* (see objc_darwin.go).

// id msgSendRect(id self, SEL sel, const NSRect *rect, a, b, c)
//
// Calls objc_msgSend(self, sel, *rect, a, b, c) where the method's signature is
// (NSRect, NSInteger-or-pointer, NSInteger-or-pointer, NSInteger-or-pointer).
// The rectangle is copied to the bottom of the outgoing stack area, where the
// callee finds it at 8(SP) past the return address, and a, b, c move down one
// register to DX, CX, R8, the integer slots after self and sel.
TEXT ·msgSendRect(SB), NOSPLIT|NOFRAME, $0-0
	SUBQ	$40, SP
	MOVQ	0(DX), AX
	MOVQ	AX, 0(SP)
	MOVQ	8(DX), AX
	MOVQ	AX, 8(SP)
	MOVQ	16(DX), AX
	MOVQ	AX, 16(SP)
	MOVQ	24(DX), AX
	MOVQ	AX, 24(SP)
	MOVQ	CX, DX
	MOVQ	R8, CX
	MOVQ	R9, R8
	CALL	gosx_objc_msgSend(SB)
	ADDQ	$40, SP
	RET

// id msgSend4F(id self, SEL sel, const double *f)
//
// Calls objc_msgSend(self, sel, f[0], f[1], f[2], f[3]) where all four are
// CGFloat arguments, which the System V ABI passes in XMM0-XMM3. A method with
// fewer floating-point arguments ignores the extra registers. Used by
// colorWithSRGBRed:green:blue:alpha: and dateWithTimeIntervalSinceNow:.
TEXT ·msgSend4F(SB), NOSPLIT|NOFRAME, $0-0
	MOVSD	0(DX), X0
	MOVSD	8(DX), X1
	MOVSD	16(DX), X2
	MOVSD	24(DX), X3
	JMP	gosx_objc_msgSend(SB)

// id msgSendZeroFP3(id self, SEL sel, a2, a3, a4, a5, stack0, stack1, stack2)
//
// Calls objc_msgSend with XMM0-XMM2 cleared. It is for a method that takes a
// zero-valued NSPoint (XMM0, XMM1) and NSTimeInterval (XMM2) ahead of integer
// arguments, namely +[NSEvent otherEventWithType:location:modifierFlags:
// timestamp:windowNumber:context:subtype:data1:data2:]. The integer arguments
// are already where Go put them: the runtime's syscallN trampoline fills DI,
// SI, DX, CX, R8, R9 and pushes the seventh to ninth arguments on the stack,
// where this jump leaves them for the callee.
TEXT ·msgSendZeroFP3(SB), NOSPLIT|NOFRAME, $0-0
	XORPS	X0, X0
	XORPS	X1, X1
	XORPS	X2, X2
	JMP	gosx_objc_msgSend(SB)

GLOBL	·fnMsgSendRect(SB), NOPTR|RODATA, $8
DATA	·fnMsgSendRect(SB)/8, $·msgSendRect(SB)
GLOBL	·fnMsgSend4F(SB), NOPTR|RODATA, $8
DATA	·fnMsgSend4F(SB)/8, $·msgSend4F(SB)
GLOBL	·fnMsgSendZeroFP3(SB), NOPTR|RODATA, $8
DATA	·fnMsgSendZeroFP3(SB)/8, $·msgSendZeroFP3(SB)
