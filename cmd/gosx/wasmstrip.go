package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

// wasmExternalizeData reads a WASM binary and splits it into:
// 1. A minimal WASM binary with a zeroed data section (just the segment headers + zero payload)
// 2. A separate .data binary containing the raw data segment payloads
//
// At runtime, the bootstrap loader fetches the .data file and writes it into
// WASM linear memory after instantiation, before calling _start.
//
// This lets the browser compile the WASM module (code section) without waiting
// for the data section to download.
func wasmExternalizeData(wasmPath, outWasmPath, outDataPath string) error {
	src, err := os.ReadFile(wasmPath)
	if err != nil {
		return fmt.Errorf("read wasm: %w", err)
	}
	if len(src) < 8 || string(src[:4]) != "\x00asm" {
		return fmt.Errorf("not a valid wasm file")
	}

	// Parse sections, find data section, record segment payloads
	type segment struct {
		payloadOffset int // offset within the original WASM file
		payloadSize   int
		memoryOffset  int // i32.const target address in linear memory
	}

	var segments []segment
	var dataSectionStart int

	pos := 8
	for pos < len(src) {
		sectionStart := pos
		sectionID := src[pos]
		pos++
		sectionSize, n := readLEB128(src[pos:])
		pos += n
		sectionBodyStart := pos

		if sectionID == 11 { // data section
			dataSectionStart = sectionStart

			// Parse segments
			segCount, n := readLEB128(src[pos:])
			p := pos + n
			for i := 0; i < segCount; i++ {
				flags := src[p]
				p++
				memOffset := 0
				if flags == 0 {
					// i32.const expr
					p++ // skip opcode (0x41 = i32.const)
					memOffset, n = readSignedLEB128(src[p:])
					p += n
					p++ // skip end opcode (0x0b)
				}
				payloadSize, n := readLEB128(src[p:])
				p += n
				segments = append(segments, segment{
					payloadOffset: p,
					payloadSize:   payloadSize,
					memoryOffset:  memOffset,
				})
				p += payloadSize
			}
		}
		pos = sectionBodyStart + sectionSize
	}

	if dataSectionStart == 0 {
		return fmt.Errorf("no data section found")
	}

	// Write the external data file: [segCount:u32] [memOffset:u32, size:u32, payload...]...
	dataFile, err := os.Create(outDataPath)
	if err != nil {
		return fmt.Errorf("create data file: %w", err)
	}
	defer dataFile.Close()

	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(len(segments)))
	dataFile.Write(buf[:])

	totalPayload := 0
	for _, seg := range segments {
		binary.LittleEndian.PutUint32(buf[:], uint32(seg.memoryOffset))
		dataFile.Write(buf[:])
		binary.LittleEndian.PutUint32(buf[:], uint32(seg.payloadSize))
		dataFile.Write(buf[:])
		dataFile.Write(src[seg.payloadOffset : seg.payloadOffset+seg.payloadSize])
		totalPayload += seg.payloadSize
	}

	// Write the stripped WASM: replace data section payloads with zeros
	out := make([]byte, len(src))
	copy(out, src)
	for _, seg := range segments {
		for i := 0; i < seg.payloadSize; i++ {
			out[seg.payloadOffset+i] = 0
		}
	}
	if err := os.WriteFile(outWasmPath, out, 0644); err != nil {
		return fmt.Errorf("write stripped wasm: %w", err)
	}

	fmt.Printf("    Data externalized: %d segments, %d KB payload\n", len(segments), totalPayload/1024)
	return nil
}

func readLEB128(data []byte) (int, int) {
	result := 0
	shift := 0
	for i, b := range data {
		result |= int(b&0x7f) << shift
		shift += 7
		if b&0x80 == 0 {
			return result, i + 1
		}
	}
	return result, len(data)
}

func readSignedLEB128(data []byte) (int, int) {
	result := 0
	shift := 0
	var b byte
	var i int
	for i, b = range data {
		result |= int(b&0x7f) << shift
		shift += 7
		if b&0x80 == 0 {
			break
		}
	}
	if shift < 32 && b&0x40 != 0 {
		result |= -(1 << shift)
	}
	return result, i + 1
}
