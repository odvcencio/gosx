package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var wasmV1Header = []byte{0, 'a', 's', 'm', 1, 0, 0, 0}

// validateWASMFraming checks the standard binary envelope, not instruction
// semantics. Runtime ABI and browser execution gates still validate execution.
func validateWASMFraming(data []byte) error {
	if len(data) < len(wasmV1Header) || !bytes.Equal(data[:len(wasmV1Header)], wasmV1Header) {
		return fmt.Errorf("missing WebAssembly v1 header (%d bytes)", len(data))
	}
	last := byte(0)
	seen := [14]bool{}
	for offset := len(wasmV1Header); offset < len(data); {
		id := data[offset]
		offset++
		if id > 13 {
			return fmt.Errorf("unsupported WebAssembly section %d", id)
		}
		var length uint32
		complete := false
		for i := uint(0); i < 5; i++ {
			if offset == len(data) {
				return fmt.Errorf("truncated section %d length", id)
			}
			b := data[offset]
			offset++
			if i == 4 && b&0xf0 != 0 {
				return fmt.Errorf("section %d length overflows u32", id)
			}
			length |= uint32(b&0x7f) << (7 * i)
			if b&0x80 == 0 {
				complete = true
				break
			}
		}
		if !complete || uint64(length) > uint64(len(data)-offset) {
			return fmt.Errorf("truncated section %d body", id)
		}
		if id != 0 {
			// Tag (13) follows Memory (5); DataCount (12) precedes Code (10).
			rank := id
			switch {
			case id == 13:
				rank = 6
			case id >= 6 && id <= 9:
				rank = id + 1
			case id == 12:
				rank = 11
			case id == 10 || id == 11:
				rank = id + 2
			}
			if seen[id] || rank < last {
				return fmt.Errorf("duplicate or out-of-order section %d", id)
			}
			seen[id], last = true, rank
		}
		offset += int(length)
	}
	return nil
}

func validateWASMFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return validateWASMFraming(data)
}

type invalidCompilerWASMOutput struct {
	path        string
	err         error
	diagnostics string
}

func (e *invalidCompilerWASMOutput) Error() string {
	message := fmt.Sprintf("compiler exited successfully with invalid WASM output %s: %v", e.path, e.err)
	if e.diagnostics != "" {
		message += "\nCompiler diagnostics:\n" + e.diagnostics
	}
	return message
}

func (e *invalidCompilerWASMOutput) Unwrap() error { return e.err }

func runValidatedWASMCompiler(cmd *exec.Cmd, outputPath string) ([]byte, error) {
	// This path belongs to this invocation. A successful command that writes
	// nothing must not inherit a previous invocation's otherwise valid module.
	if err := os.Remove(outputPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("clear owned compiler output %s: %w", outputPath, err)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, err
	}
	if err := validateWASMFile(outputPath); err != nil {
		return output, &invalidCompilerWASMOutput{outputPath, err, strings.TrimSpace(string(output))}
	}
	return output, nil
}
