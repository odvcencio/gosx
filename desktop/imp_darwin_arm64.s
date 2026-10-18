//go:build darwin && !ios && arm64

#include "textflag.h"
#include "go_asm.h"

// The delegate recorder and its stubs, arm64.
//
// AppKit and WebKit call an Objective-C delegate method as a C function,
// IMP(self, _cmd, args...), on the main thread. Go cannot be that function (see
// ctrl_darwin.go), so every method GoSXDelegate and GoSXMenuTarget register points
// at a stub here.
// A stub loads an event kind and a retain mask into R9 and R10 and branches to
// impRecord, which appends one record to the ring in the ctrl block, retains the
// arguments the mask names, publishes the record and returns 0. The return value
// is the conservative answer for the methods that must answer synchronously
// (NO from windowShouldClose:, NSTerminateCancel from applicationShouldTerminate:)
// and is ignored by the void ones (webView:didFinishNavigation:, menuAction:). Go acts on the record later, on its own
// thread.
//
// This is code AppKit calls, so:
//   - NOSPLIT|NOFRAME: no stack check, which would read g. The Go runtime may be
//     in a syscall on this very thread, with g0 current and TLS g unrelated to
//     anything this code can use.
//   - It never touches R18 (the platform register), R26 (closure context), R27
//     (the assembler's temporary, callee-saved under AAPCS64) or R28 (g), and it
//     never takes the address of a symbol (which could use R27). The only
//     symbol reference is a direct branch from a stub to impRecord. Everything
//     else is read through the ctrl block, whose offsets come from go_asm.h.
//     R0-R17 are scratch; R19-R24 are saved.
//   - The only calls are through pointers in the ctrl block: ctrl.retain
//     (objc_retain) and ctrl.wakeFn (objc_msgSend, for [NSApp postEvent:atStart:]).
//     Neither re-enters Go.
//   - SP is 16-byte aligned throughout.

// id impRecord(id self, SEL _cmd, id a0, id a1, id a2)  with R9 = kind, R10 = retain mask
//
//	ctrl = *(self + 8)                          the "ctrl" instance variable
//	if ctrl == nil:                  return 0   an instance not wired to a ring yet
//	if retain == nil:                mask = 0
//	if head - tail > mask:           dropped++, return 0   (acquire load of tail)
//	ring[head & mask] = {kind, _cmd, a0, a1, a2, mask}
//	for each bit i of mask:          retain(a_i), retains++
//	calls++; head++                  (release store of head)
//	if wakeFn != nil:                wakeFn(wakeSelf, wakeSel, wakeEvent, 1)
//	return 0
TEXT ·impRecord(SB), NOSPLIT|NOFRAME, $0-0
	STP.W	(R29, R30), -64(RSP)
	MOVD	RSP, R29
	STP	(R19, R20), 16(RSP)
	STP	(R21, R22), 32(RSP)
	STP	(R23, R24), 48(RSP)

	MOVD	8(R0), R19			// ctrl
	CBZ	R19, done

	// A ring without a retain function records the arguments but retains
	// nothing, so Go has nothing to release.
	MOVD	ctrl_retain(R19), R22
	CBNZ	R22, hasretain
	MOVD	ZR, R10
hasretain:
	MOVD	R10, R23			// retain mask, kept across the retain calls

	// Room check: head - tail <= mask. Only this function writes head, so a
	// plain load sees the current value; tail is Go's, loaded with acquire.
	MOVD	ctrl_head(R19), R20
	ADD	$ctrl_tail, R19, R11
	LDAR	(R11), R11
	SUB	R11, R20, R11			// records in the ring
	MOVD	ctrl_mask(R19), R12
	CMP	R12, R11
	BHI	full

	// R21 = &ring[head & mask]
	AND	R12, R20, R13
	MOVD	$nativeEvent__size, R14
	MUL	R14, R13, R13
	MOVD	ctrl_data(R19), R14
	ADD	R13, R14, R21

	MOVD	R9, nativeEvent_kind(R21)
	MOVD	R1, nativeEvent_sel(R21)
	MOVD	R2, (nativeEvent_args+0)(R21)
	MOVD	R3, (nativeEvent_args+8)(R21)
	MOVD	R4, (nativeEvent_args+16)(R21)
	MOVD	R23, nativeEvent_mask(R21)

	TBZ	$0, R23, retain1
	MOVD	(nativeEvent_args+0)(R21), R0
	BL	(R22)
	MOVD	ctrl_retains(R19), R11
	ADD	$1, R11
	MOVD	R11, ctrl_retains(R19)
retain1:
	TBZ	$1, R23, retain2
	MOVD	(nativeEvent_args+8)(R21), R0
	BL	(R22)
	MOVD	ctrl_retains(R19), R11
	ADD	$1, R11
	MOVD	R11, ctrl_retains(R19)
retain2:
	TBZ	$2, R23, publish
	MOVD	(nativeEvent_args+16)(R21), R0
	BL	(R22)
	MOVD	ctrl_retains(R19), R11
	ADD	$1, R11
	MOVD	R11, ctrl_retains(R19)

publish:
	MOVD	ctrl_calls(R19), R11
	ADD	$1, R11
	MOVD	R11, ctrl_calls(R19)
	ADD	$1, R20, R20
	ADD	$ctrl_head, R19, R11
	STLR	R20, (R11)			// release: the record and the retains are done

	// Wake the event pump, which does not return for a run-loop-source callback.
	MOVD	ctrl_wakeFn(R19), R9
	CBZ	R9, done
	MOVD	ctrl_wakeSelf(R19), R0
	MOVD	ctrl_wakeSel(R19), R1
	MOVD	ctrl_wakeEvent(R19), R2
	MOVD	$1, R3				// atStart:YES
	BL	(R9)
	B	done

full:
	MOVD	ctrl_dropped(R19), R11
	ADD	$1, R11
	MOVD	R11, ctrl_dropped(R19)

done:
	MOVD	ZR, R0
	LDP	16(RSP), (R19, R20)
	LDP	32(RSP), (R21, R22)
	LDP	48(RSP), (R23, R24)
	LDP.P	64(RSP), (R29, R30)
	RET

// Stubs. Each is the whole method implementation: it names what happened and
// which arguments to retain, then becomes impRecord.

// BOOL windowShouldClose:(NSWindow *sender)        -> NO, record closeRequested
TEXT ·impWindowShouldClose(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	$const_evCloseRequested, R9
	MOVD	$const_retainNone, R10
	B	·impRecord(SB)

// NSApplicationTerminateReply applicationShouldTerminate:(NSApplication *sender)
//                                                  -> NSTerminateCancel, record quitRequested
TEXT ·impApplicationShouldTerminate(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	$const_evQuitRequested, R9
	MOVD	$const_retainNone, R10
	B	·impRecord(SB)

// void webView:(WKWebView *)w didFinishNavigation:(WKNavigation *)n
TEXT ·impNavigationFinished(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	$const_evNavigationFinished, R9
	MOVD	$const_retainNone, R10
	B	·impRecord(SB)

// void webView:(WKWebView *)w didFailNavigation:(WKNavigation *)n withError:(NSError *)e
// void webView:(WKWebView *)w didFailProvisionalNavigation:(WKNavigation *)n withError:(NSError *)e
// Retains the NSError (a2): Go reads its code after the method has returned.
TEXT ·impNavigationFailed(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	$const_evNavigationFailed, R9
	MOVD	$const_retainArg2, R10
	B	·impRecord(SB)

// void menuAction:(NSMenuItem *sender)
// Retains the sender (a0): Go reads its tag after the method has returned, and the
// item may be released by then if the menu bar was replaced in between.
TEXT ·impMenuAction(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	$const_evMenuAction, R9
	MOVD	$const_retainArg0, R10
	B	·impRecord(SB)

// Finder supplies an NSArray as argument 1; retain it until Go drains it.
TEXT ·impOpenFiles(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	$const_evOpenFiles, R9
	MOVD	$const_retainArg1, R10
	B	·impRecord(SB)

TEXT ·impOpenURLs(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	$const_evOpenURLs, R9
	MOVD	$const_retainArg1, R10
	B	·impRecord(SB)

TEXT ·impWebMessage(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	$const_evWebMessage, R9
	MOVD	$const_retainArg1, R10
	B	·impRecord(SB)

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
