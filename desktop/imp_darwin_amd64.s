//go:build darwin && !ios && amd64

#include "textflag.h"
#include "go_asm.h"

// The delegate recorder and its stubs, amd64. The algorithm is the one in
// imp_darwin_arm64.s; read the comment there.
//
// Under the System V ABI an Objective-C method arrives as IMP(self, _cmd, a0,
// a1, a2) in DI, SI, DX, CX, R8. A stub loads the event kind into R9 and the
// retain mask into R10 (both free: the methods take at most five arguments) and
// jumps to impRecord.
//
// This is code AppKit calls, so:
//   - NOSPLIT|NOFRAME: no stack check, which would read g.
//   - It never refers to a symbol except the stubs' jump to impRecord and never
//     reads g (R14 is only a callee-saved register here, restored before
//     returning). Everything is read through the ctrl block, whose offsets come
//     from go_asm.h.
//   - BX, BP and R12-R15 are callee-saved under the C ABI and are saved. Entry
//     SP is 8 mod 16 and the 56-byte frame makes it 0, so SP is 16-byte aligned
//     at the call, as the C ABI requires. The frame is built with SUBQ and MOVQ
//     rather than PUSHQ/POPQ because the assembler checks PUSH/POP balance in
//     program order, and this function has two exits.
//   - x86 orders loads and stores strongly enough that plain MOVs give the
//     acquire load of tail and the release store of head.
//   - The only calls are through pointers in the ctrl block: ctrl.retain
//     (objc_retain) and ctrl.wakeFn (objc_msgSend, for [NSApp postEvent:atStart:]).
//     Neither re-enters Go.

// id impRecord(id self, SEL _cmd, id a0, id a1, id a2)  with R9 = kind, R10 = retain mask
TEXT ·impRecord(SB), NOSPLIT|NOFRAME, $0-0
	SUBQ	$56, SP
	MOVQ	BP, 48(SP)
	LEAQ	48(SP), BP
	MOVQ	BX, 0(SP)
	MOVQ	R12, 8(SP)
	MOVQ	R13, 16(SP)
	MOVQ	R14, 24(SP)
	MOVQ	R15, 32(SP)

	MOVQ	8(DI), BX			// ctrl, from the "ctrl" instance variable
	TESTQ	BX, BX
	JEQ	done

	// A ring without a retain function records the arguments but retains
	// nothing, so Go has nothing to release.
	MOVQ	ctrl_retain(BX), R15
	MOVQ	R10, R14			// retain mask, kept across the retain calls
	TESTQ	R15, R15
	JNE	hasretain
	XORL	R14, R14
hasretain:

	// Room check: head - tail <= mask. Only this function writes head.
	MOVQ	ctrl_head(BX), R12
	MOVQ	ctrl_tail(BX), AX
	MOVQ	R12, R11
	SUBQ	AX, R11				// records in the ring
	CMPQ	R11, ctrl_mask(BX)
	JHI	full

	// R13 = &ring[head & mask]
	MOVQ	R12, AX
	ANDQ	ctrl_mask(BX), AX
	IMULQ	$nativeEvent__size, AX
	ADDQ	ctrl_data(BX), AX
	MOVQ	AX, R13

	MOVQ	R9, nativeEvent_kind(R13)
	MOVQ	SI, nativeEvent_sel(R13)
	MOVQ	DX, (nativeEvent_args+0)(R13)
	MOVQ	CX, (nativeEvent_args+8)(R13)
	MOVQ	R8, (nativeEvent_args+16)(R13)
	MOVQ	R14, nativeEvent_mask(R13)

	TESTQ	$1, R14
	JEQ	retain1
	MOVQ	(nativeEvent_args+0)(R13), DI
	CALL	R15
	INCQ	ctrl_retains(BX)
retain1:
	TESTQ	$2, R14
	JEQ	retain2
	MOVQ	(nativeEvent_args+8)(R13), DI
	CALL	R15
	INCQ	ctrl_retains(BX)
retain2:
	TESTQ	$4, R14
	JEQ	publish
	MOVQ	(nativeEvent_args+16)(R13), DI
	CALL	R15
	INCQ	ctrl_retains(BX)

publish:
	INCQ	ctrl_calls(BX)
	INCQ	R12
	MOVQ	R12, ctrl_head(BX)		// release: the record and the retains are done

	// Wake the event pump, which does not return for a run-loop-source callback.
	MOVQ	ctrl_wakeFn(BX), AX
	TESTQ	AX, AX
	JEQ	done
	MOVQ	ctrl_wakeSelf(BX), DI
	MOVQ	ctrl_wakeSel(BX), SI
	MOVQ	ctrl_wakeEvent(BX), DX
	MOVL	$1, CX				// atStart:YES
	CALL	AX
	JMP	done

full:
	INCQ	ctrl_dropped(BX)

done:
	XORL	AX, AX
	MOVQ	0(SP), BX
	MOVQ	8(SP), R12
	MOVQ	16(SP), R13
	MOVQ	24(SP), R14
	MOVQ	32(SP), R15
	MOVQ	48(SP), BP
	ADDQ	$56, SP
	RET

// Stubs. Each is the whole method implementation: it names what happened and
// which arguments to retain, then becomes impRecord.

// BOOL windowShouldClose:(NSWindow *sender)        -> NO, record closeRequested
TEXT ·impWindowShouldClose(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	$const_evCloseRequested, R9
	MOVQ	$const_retainNone, R10
	JMP	·impRecord(SB)

// NSApplicationTerminateReply applicationShouldTerminate:(NSApplication *sender)
//                                                  -> NSTerminateCancel, record quitRequested
TEXT ·impApplicationShouldTerminate(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	$const_evQuitRequested, R9
	MOVQ	$const_retainNone, R10
	JMP	·impRecord(SB)

// void webView:(WKWebView *)w didFinishNavigation:(WKNavigation *)n
TEXT ·impNavigationFinished(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	$const_evNavigationFinished, R9
	MOVQ	$const_retainNone, R10
	JMP	·impRecord(SB)

// void webView:(WKWebView *)w didFailNavigation:(WKNavigation *)n withError:(NSError *)e
// void webView:(WKWebView *)w didFailProvisionalNavigation:(WKNavigation *)n withError:(NSError *)e
// Retains the NSError (a2): Go reads its code after the method has returned.
TEXT ·impNavigationFailed(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	$const_evNavigationFailed, R9
	MOVQ	$const_retainArg2, R10
	JMP	·impRecord(SB)

// void menuAction:(NSMenuItem *sender)
// Retains the sender (a0): Go reads its tag after the method has returned, and the
// item may be released by then if the menu bar was replaced in between.
TEXT ·impMenuAction(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	$const_evMenuAction, R9
	MOVQ	$const_retainArg0, R10
	JMP	·impRecord(SB)

// Finder supplies an NSArray as argument 1; retain it until Go drains it.
TEXT ·impOpenFiles(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	$const_evOpenFiles, R9
	MOVQ	$const_retainArg1, R10
	JMP	·impRecord(SB)

TEXT ·impOpenURLs(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	$const_evOpenURLs, R9
	MOVQ	$const_retainArg1, R10
	JMP	·impRecord(SB)

TEXT ·impWebMessage(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	$const_evWebMessage, R9
	MOVQ	$const_retainArg1, R10
	JMP	·impRecord(SB)

// impStubs publishes the C-ABI entry of each stub, in the order of the
// impStub* constants. Read from Go as a uintptr array: the func value of an
// assembly function points at a wrapper.
GLOBL	·impStubs(SB), NOPTR|RODATA, $96
DATA	·impStubs+0(SB)/8, $·impWindowShouldClose(SB)
DATA	·impStubs+8(SB)/8, $·impApplicationShouldTerminate(SB)
DATA	·impStubs+16(SB)/8, $·impNavigationFinished(SB)
DATA	·impStubs+24(SB)/8, $·impNavigationFailed(SB)
DATA	·impStubs+32(SB)/8, $·impMenuAction(SB)
DATA	·impStubs+40(SB)/8, $·impOpenFiles(SB)
DATA	·impStubs+48(SB)/8, $·impOpenURLs(SB)
DATA	·impStubs+56(SB)/8, $·impWebMessage(SB)
DATA	·impStubs+64(SB)/8, $·impWebFilePanel(SB)
DATA	·impStubs+72(SB)/8, $·impWebAlert(SB)
DATA	·impStubs+80(SB)/8, $·impWebConfirm(SB)
DATA	·impStubs+88(SB)/8, $·impWebPrompt(SB)
