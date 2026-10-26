//go:build darwin && !ios && arm64

#include "textflag.h"
#include "go_asm.h"

// WKUIDelegate methods copy the completion Block before returning to WebKit.
// Native overflow/nil-controller paths invoke cancellation synchronously, so
// no completion can be lost even when Go cannot receive an event.
// Register/stack discipline is the same as impRecord in imp_darwin_arm64.s.
TEXT ·impWebUIRecord(SB), NOSPLIT|NOFRAME, $0-0
	STP.W	(R29, R30), -64(RSP)
	MOVD	RSP, R29
	STP	(R19, R20), 16(RSP)
	STP	(R21, R22), 32(RSP)
	STP	(R23, R24), 48(RSP)

	MOVD	R6, R24			// original completion Block
	CBZ	R24, done
	MOVD	8(R0), R19			// ctrl
	CBZ	R19, cancel

	// A ring without a retain function records the arguments but retains
	// nothing, so Go has nothing to release.
	MOVD	ctrl_retain(R19), R22
	CBNZ	R22, hasretain
	MOVD	ZR, R10
hasretain:
	MOVD	R10, R23			// retain mask, kept across the retain calls
	ORR	$const_copyCompletion, R23, R23
	MOVD	ctrl_blockCopy(R19), R11
	CBZ	R11, cancel

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
	TBZ	$2, R23, copyblock
	MOVD	(nativeEvent_args+16)(R21), R0
	BL	(R22)
	MOVD	ctrl_retains(R19), R11
	ADD	$1, R11
	MOVD	R11, ctrl_retains(R19)

copyblock:
	MOVD	R24, R0
	MOVD	ctrl_blockCopy(R19), R11
	BL	(R11)
	MOVD	R0, nativeEvent_completion(R21)
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

cancel:
	MOVD	R24, R0
	CBZ	R0, done
	MOVD	16(R0), R11		// Block invoke pointer
	CBZ	R11, done
	MOVD	ZR, R1			// cancel = nil/NO
	BL	(R11)

done:
	MOVD	ZR, R0
	LDP	16(RSP), (R19, R20)
	LDP	32(RSP), (R21, R22)
	LDP	48(RSP), (R23, R24)
	LDP.P	64(RSP), (R29, R30)
	RET

TEXT ·impWebFilePanel(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	R5, R6
	MOVD	R3, R2
	MOVD	R4, R3
	MOVD	ZR, R4
	MOVD	$const_evWebFilePanel, R9
	MOVD	$3, R10
	B	·impWebUIRecord(SB)

TEXT ·impWebAlert(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	R5, R6
	MOVD	R3, R2
	MOVD	R4, R3
	MOVD	ZR, R4
	MOVD	$const_evWebAlert, R9
	MOVD	$3, R10
	B	·impWebUIRecord(SB)

TEXT ·impWebConfirm(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	R5, R6
	MOVD	R3, R2
	MOVD	R4, R3
	MOVD	ZR, R4
	MOVD	$const_evWebConfirm, R9
	MOVD	$3, R10
	B	·impWebUIRecord(SB)

TEXT ·impWebPrompt(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	R3, R2
	MOVD	R4, R3
	MOVD	R5, R4
	MOVD	$const_evWebPrompt, R9
	MOVD	$7, R10
	B	·impWebUIRecord(SB)

// C-ABI Block invocation, reached through syscall.syscall from Go. Extra result
// is ignored by alert's void(void) completion; other completions take one value.
TEXT ·webBlockInvoke(SB), NOSPLIT|NOFRAME, $0-0
	CBZ R0, blockdone
	MOVD 16(R0), R2
	CBZ R2, blockdone
	B (R2)
blockdone:
	RET
GLOBL ·fnBlockInvoke(SB), RODATA, $8
DATA ·fnBlockInvoke(SB)/8, $·webBlockInvoke(SB)
