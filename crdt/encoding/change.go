package encoding

import "crypto/sha256"

// EncodeChange wraps a change body in a change chunk.
func EncodeChange(body []byte) []byte {
	return encodeChunk(ChangeChunkType, body)
}

// DecodeChange unwraps either a plain or compressed change chunk body.
func DecodeChange(data []byte) ([]byte, error) {
	chunkType, body, err := decodeChunk(data)
	if err != nil {
		return nil, err
	}
	if chunkType != ChangeChunkType && chunkType != CompressedChangeChunkType {
		return nil, errUnexpectedChangeType(chunkType)
	}
	return body, nil
}

// ChangeHash computes the DAG hash for a change body using the Automerge chunk rule.
func ChangeHash(body []byte) [32]byte {
	length := AppendULEB128(nil, uint64(len(body)))
	return sha256.Sum256(append([]byte{ChangeChunkType}, append(length, body...)...))
}

func errUnexpectedChangeType(chunkType byte) error {
	return &chunkTypeError{chunkType: chunkType}
}

type chunkTypeError struct {
	chunkType byte
}

func (e *chunkTypeError) Error() string {
	return "unexpected change chunk type"
}
