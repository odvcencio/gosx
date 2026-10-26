//go:build darwin && !ios && amd64

#include "textflag.h"

// System V C ABI versions of the native-only Blocks test callbacks.
TEXT web_test_block_invoke<>(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	32(DI), AX
	INCQ	0(AX)
	MOVQ	SI, 8(AX)
	RET

TEXT web_test_block_copy<>(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	32(SI), AX
	INCQ	16(AX)
	RET

TEXT web_test_block_dispose<>(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	32(DI), AX
	INCQ	24(AX)
	RET

TEXT web_test_dlsym<>(SB), NOSPLIT|NOFRAME, $0-0
	JMP	gosx_web_test_dlsym(SB)

GLOBL	·fnWebTestBlockInvoke(SB), NOPTR|RODATA, $8
DATA	·fnWebTestBlockInvoke(SB)/8, $web_test_block_invoke<>(SB)
GLOBL	·fnWebTestBlockCopy(SB), NOPTR|RODATA, $8
DATA	·fnWebTestBlockCopy(SB)/8, $web_test_block_copy<>(SB)
GLOBL	·fnWebTestBlockDispose(SB), NOPTR|RODATA, $8
DATA	·fnWebTestBlockDispose(SB)/8, $web_test_block_dispose<>(SB)
GLOBL	·fnWebTestDlsym(SB), NOPTR|RODATA, $8
DATA	·fnWebTestDlsym(SB)/8, $web_test_dlsym<>(SB)
