//go:build darwin && !ios && (amd64 || arm64)

#include "textflag.h"

TEXT gosx_Block_copy_trampoline<>(SB),NOSPLIT,$0-0
	JMP gosx_Block_copy(SB)
GLOBL ·fnBlockCopy(SB), RODATA, $8
DATA ·fnBlockCopy(SB)/8, $gosx_Block_copy_trampoline<>(SB)

TEXT gosx_Block_release_trampoline<>(SB),NOSPLIT,$0-0
	JMP gosx_Block_release(SB)
GLOBL ·fnBlockRelease(SB), RODATA, $8
DATA ·fnBlockRelease(SB)/8, $gosx_Block_release_trampoline<>(SB)
