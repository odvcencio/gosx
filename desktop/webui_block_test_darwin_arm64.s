//go:build darwin && !ios && arm64

#include "textflag.h"

// C ABI callbacks used only by webui_block_darwin_test.go. Each captured state
// lives in mmap memory. No callback can enter Go or depend on a Go stack.
TEXT web_test_block_invoke<>(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	32(R0), R2
	MOVD	0(R2), R3
	ADD	$1, R3
	MOVD	R3, 0(R2)
	MOVD	R1, 8(R2)
	RET

TEXT web_test_block_copy<>(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	32(R1), R2
	MOVD	16(R2), R3
	ADD	$1, R3
	MOVD	R3, 16(R2)
	RET

TEXT web_test_block_dispose<>(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	32(R0), R2
	MOVD	24(R2), R3
	ADD	$1, R3
	MOVD	R3, 24(R2)
	RET

TEXT web_test_dlsym<>(SB), NOSPLIT|NOFRAME, $0-0
	B	gosx_web_test_dlsym(SB)

GLOBL	·fnWebTestBlockInvoke(SB), NOPTR|RODATA, $8
DATA	·fnWebTestBlockInvoke(SB)/8, $web_test_block_invoke<>(SB)
GLOBL	·fnWebTestBlockCopy(SB), NOPTR|RODATA, $8
DATA	·fnWebTestBlockCopy(SB)/8, $web_test_block_copy<>(SB)
GLOBL	·fnWebTestBlockDispose(SB), NOPTR|RODATA, $8
DATA	·fnWebTestBlockDispose(SB)/8, $web_test_block_dispose<>(SB)
GLOBL	·fnWebTestDlsym(SB), NOPTR|RODATA, $8
DATA	·fnWebTestDlsym(SB)/8, $web_test_dlsym<>(SB)
