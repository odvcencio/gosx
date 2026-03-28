package encoding

import (
	"bytes"
	"crypto/sha256"
	"fmt"
)

var magicBytes = []byte{0x85, 0x6f, 0x4a, 0x83}

const (
	DocumentChunkType         = 0x00
	ChangeChunkType           = 0x01
	CompressedChangeChunkType = 0x02
)

// EncodeDocument wraps a snapshot body in a document chunk.
func EncodeDocument(body []byte) []byte {
	return encodeChunk(DocumentChunkType, body)
}

// DecodeDocument unwraps a document chunk and returns its body.
func DecodeDocument(data []byte) ([]byte, error) {
	chunkType, body, err := decodeChunk(data)
	if err != nil {
		return nil, err
	}
	if chunkType != DocumentChunkType {
		return nil, fmt.Errorf("unexpected chunk type %x", chunkType)
	}
	return body, nil
}

func encodeChunk(chunkType byte, body []byte) []byte {
	length := AppendULEB128(nil, uint64(len(body)))
	checksum := sha256.Sum256(hashInput(chunkType, length, body))

	out := make([]byte, 0, len(magicBytes)+1+len(length)+4+len(body))
	out = append(out, magicBytes...)
	out = append(out, chunkType)
	out = append(out, length...)
	out = append(out, checksum[:4]...)
	out = append(out, body...)
	return out
}

func decodeChunk(data []byte) (byte, []byte, error) {
	if len(data) < len(magicBytes)+1+4 {
		return 0, nil, fmt.Errorf("chunk too short")
	}
	if !bytes.Equal(data[:len(magicBytes)], magicBytes) {
		return 0, nil, fmt.Errorf("invalid chunk magic")
	}

	offset := len(magicBytes)
	chunkType := data[offset]
	offset++

	bodyLen, n, err := ReadULEB128(data[offset:])
	if err != nil {
		return 0, nil, err
	}
	lengthBytes := append([]byte(nil), data[offset:offset+n]...)
	offset += n

	if len(data[offset:]) < 4 {
		return 0, nil, fmt.Errorf("chunk missing checksum")
	}
	checksum := data[offset : offset+4]
	offset += 4

	if len(data[offset:]) != int(bodyLen) {
		return 0, nil, fmt.Errorf("chunk body length mismatch: got %d want %d", len(data[offset:]), bodyLen)
	}
	body := data[offset:]

	expected := sha256.Sum256(hashInput(chunkType, lengthBytes, body))
	if !bytes.Equal(checksum, expected[:4]) {
		return 0, nil, fmt.Errorf("chunk checksum mismatch")
	}
	return chunkType, append([]byte(nil), body...), nil
}

func hashInput(chunkType byte, length []byte, body []byte) []byte {
	out := make([]byte, 0, 1+len(length)+len(body))
	out = append(out, chunkType)
	out = append(out, length...)
	out = append(out, body...)
	return out
}
