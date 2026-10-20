//go:build darwin && !ios && amd64

#include "textflag.h"
#include "go_asm.h"

// WKUIDelegate methods copy the completion Block before returning to WebKit.
// Native overflow/nil-controller paths invoke cancellation synchronously, so
// no completion can be lost even when Go cannot receive an event.
// Register/stack discipline is the same as impRecord in imp_darwin_amd64.s.
TEXT ·impWebUIRecord(SB), NOSPLIT|NOFRAME, $0-0
	SUBQ	$56, SP
	MOVQ	BP, 48(SP)
	LEAQ	48(SP), BP
	MOVQ	BX, 0(SP)
	MOVQ	R12, 8(SP)
	MOVQ	R13, 16(SP)
	MOVQ	R14, 24(SP)
	MOVQ	R15, 32(SP)

	MOVQ	R11, 40(SP)		// original completion Block
	TESTQ	R11, R11
	JEQ	done
	MOVQ	8(DI), BX			// ctrl, from the "ctrl" instance variable
	TESTQ	BX, BX
	JEQ	cancel

	// A ring without a retain function records the arguments but retains
	// nothing, so Go has nothing to release.
	MOVQ	ctrl_retain(BX), R15
	MOVQ	R10, R14			// retain mask, kept across the retain calls
	TESTQ	R15, R15
	JNE	hasretain
	XORL	R14, R14
hasretain:
	ORQ	$const_copyCompletion, R14
	MOVQ	ctrl_blockCopy(BX), AX
	TESTQ	AX, AX
	JEQ	cancel

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
	JEQ	copyblock
	MOVQ	(nativeEvent_args+16)(R13), DI
	CALL	R15
	INCQ	ctrl_retains(BX)

copyblock:
	MOVQ	40(SP), DI
	MOVQ	ctrl_blockCopy(BX), AX
	CALL	AX
	MOVQ	AX, nativeEvent_completion(R13)
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

cancel:
	MOVQ	40(SP), DI
	TESTQ	DI, DI
	JEQ	done
	MOVQ	16(DI), AX		// Block invoke pointer
	TESTQ	AX, AX
	JEQ	done
	XORL	SI, SI			// cancel = nil/NO
	CALL	AX

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

TEXT ·impWebFilePanel(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	R9, R11
	MOVQ	CX, DX
	MOVQ	R8, CX
	XORL	R8, R8
	MOVQ	$const_evWebFilePanel, R9
	MOVQ	$3, R10
	JMP	·impWebUIRecord(SB)

TEXT ·impWebAlert(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	R9, R11
	MOVQ	CX, DX
	MOVQ	R8, CX
	XORL	R8, R8
	MOVQ	$const_evWebAlert, R9
	MOVQ	$3, R10
	JMP	·impWebUIRecord(SB)

TEXT ·impWebConfirm(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	R9, R11
	MOVQ	CX, DX
	MOVQ	R8, CX
	XORL	R8, R8
	MOVQ	$const_evWebConfirm, R9
	MOVQ	$3, R10
	JMP	·impWebUIRecord(SB)

TEXT ·impWebPrompt(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	8(SP), R11
	MOVQ	CX, DX
	MOVQ	R8, CX
	MOVQ	R9, R8
	MOVQ	$const_evWebPrompt, R9
	MOVQ	$7, R10
	JMP	·impWebUIRecord(SB)

TEXT ·webBlockInvoke(SB), NOSPLIT|NOFRAME, $0-0
	TESTQ DI, DI
	JEQ blockdone
	MOVQ 16(DI), AX
	TESTQ AX, AX
	JEQ blockdone
	JMP AX
blockdone:
	RET
GLOBL ·fnBlockInvoke(SB), RODATA, $8
DATA ·fnBlockInvoke(SB)/8, $·webBlockInvoke(SB)
