package wasm

import (
	"encoding/binary"
	"errors"
	"fmt"

	"m31labs.dev/gosx/client/vm"
)

const (
	// MailboxMagic is the little-endian wire marker for GoSX mailboxes ("GSXM").
	MailboxMagic uint32 = 0x4d585347

	// MailboxHeaderSize is the fixed prefix for every request and response.
	MailboxHeaderSize = 24

	// MailboxFlagResponse marks a mailbox written by the runtime rather than
	// one sent by the browser.
	MailboxFlagResponse uint32 = 1 << 0

	// MailboxStatusOK is the only success status currently defined.
	MailboxStatusOK int32 = 0

	// Keep malformed browser input bounded before allocating a payload-sized
	// buffer.  Scene programs can be large, but direct messages are not.
	MaxMailboxPayload uint32 = 64 << 20
)

// Opcode names the binary operation carried by a mailbox.
type Opcode uint16

const (
	MailboxOpcodeHandshake Opcode = iota + 1
	MailboxOpcodePing
	MailboxOpcodePatches
	MailboxOpcodeAction
	MailboxOpcodeInputBatch
	MailboxOpcodeEngineFrame
)

// MailboxHeader is serialized little-endian in exactly MailboxHeaderSize
// bytes.  The explicit fields make the layout auditable from TypeScript.
type MailboxHeader struct {
	Magic       uint32
	Version     uint16
	Opcode      Opcode
	RequestID   uint32
	PayloadSize uint32
	Status      int32
	Flags       uint32
}

// EncodeMailbox creates one complete mailbox frame.
func EncodeMailbox(opcode Opcode, requestID uint32, status int32, flags uint32, payload []byte) ([]byte, error) {
	if uint64(len(payload)) > uint64(MaxMailboxPayload) {
		return nil, fmt.Errorf("mailbox payload is too large: %d", len(payload))
	}
	header := MailboxHeader{
		Magic:       MailboxMagic,
		Version:     MailboxVersion,
		Opcode:      opcode,
		RequestID:   requestID,
		PayloadSize: uint32(len(payload)),
		Status:      status,
		Flags:       flags,
	}
	out := make([]byte, MailboxHeaderSize+len(payload))
	binary.LittleEndian.PutUint32(out[0:4], header.Magic)
	binary.LittleEndian.PutUint16(out[4:6], header.Version)
	binary.LittleEndian.PutUint16(out[6:8], uint16(header.Opcode))
	binary.LittleEndian.PutUint32(out[8:12], header.RequestID)
	binary.LittleEndian.PutUint32(out[12:16], header.PayloadSize)
	binary.LittleEndian.PutUint32(out[16:20], uint32(header.Status))
	binary.LittleEndian.PutUint32(out[20:24], header.Flags)
	copy(out[MailboxHeaderSize:], payload)
	return out, nil
}

// DecodeMailbox validates and splits one complete mailbox frame.
func DecodeMailbox(data []byte) (MailboxHeader, []byte, error) {
	if len(data) < MailboxHeaderSize {
		return MailboxHeader{}, nil, errors.New("mailbox is shorter than its header")
	}
	header := MailboxHeader{
		Magic:       binary.LittleEndian.Uint32(data[0:4]),
		Version:     binary.LittleEndian.Uint16(data[4:6]),
		Opcode:      Opcode(binary.LittleEndian.Uint16(data[6:8])),
		RequestID:   binary.LittleEndian.Uint32(data[8:12]),
		PayloadSize: binary.LittleEndian.Uint32(data[12:16]),
		Status:      int32(binary.LittleEndian.Uint32(data[16:20])),
		Flags:       binary.LittleEndian.Uint32(data[20:24]),
	}
	if header.Magic != MailboxMagic {
		return MailboxHeader{}, nil, fmt.Errorf("mailbox magic 0x%x is invalid", header.Magic)
	}
	if header.Version != MailboxVersion {
		return MailboxHeader{}, nil, fmt.Errorf("mailbox version %d is unsupported", header.Version)
	}
	if header.PayloadSize > MaxMailboxPayload {
		return MailboxHeader{}, nil, fmt.Errorf("mailbox payload %d exceeds limit", header.PayloadSize)
	}
	want := uint64(MailboxHeaderSize) + uint64(header.PayloadSize)
	if want != uint64(len(data)) {
		return MailboxHeader{}, nil, fmt.Errorf("mailbox length %d does not match header payload %d", len(data), header.PayloadSize)
	}
	return header, data[MailboxHeaderSize:], nil
}

// EncodeHandshakePayload is the binary payload for MailboxOpcodeHandshake.
func EncodeHandshakePayload(handshake Handshake) ([]byte, error) {
	var out []byte
	out = appendU32(out, handshake.ABIVersion)
	out = appendU32(out, uint32(handshake.FeatureMask))
	out = appendU16(out, handshake.MailboxVersion)
	var err error
	out, err = appendString(out, string(handshake.Variant))
	if err != nil {
		return nil, err
	}
	return appendString(out, handshake.ManifestHash)
}

// DecodeHandshakePayload decodes the payload without using JSON.
func DecodeHandshakePayload(payload []byte) (Handshake, error) {
	reader := mailboxReader{data: payload}
	abi, err := reader.u32()
	if err != nil {
		return Handshake{}, err
	}
	features, err := reader.u32()
	if err != nil {
		return Handshake{}, err
	}
	mailboxVersion, err := reader.u16()
	if err != nil {
		return Handshake{}, err
	}
	variant, err := reader.string()
	if err != nil {
		return Handshake{}, err
	}
	manifestHash, err := reader.string()
	if err != nil {
		return Handshake{}, err
	}
	if reader.remaining() != 0 {
		return Handshake{}, errors.New("handshake payload has trailing bytes")
	}
	return Handshake{
		ABIVersion:     abi,
		FeatureMask:    FeatureMask(features),
		MailboxVersion: mailboxVersion,
		Variant:        Variant(variant),
		ManifestHash:   manifestHash,
	}, nil
}

// EncodePatchPayload encodes an island id and its patch list.  Strings are
// length-prefixed UTF-8 byte sequences; integers are little-endian.
func EncodePatchPayload(islandID string, patches []vm.PatchOp) ([]byte, error) {
	out, err := appendString(nil, islandID)
	if err != nil {
		return nil, err
	}
	if uint64(len(patches)) > uint64(^uint32(0)) {
		return nil, errors.New("patch count exceeds mailbox limit")
	}
	out = appendU32(out, uint32(len(patches)))
	for i := range patches {
		op := &patches[i]
		out = append(out, byte(op.Kind))
		for _, value := range []string{op.Path, op.Tag, op.Text, op.AttrName} {
			out, err = appendString(out, value)
			if err != nil {
				return nil, err
			}
		}
		if uint64(len(op.Children)) > uint64(^uint32(0)) {
			return nil, errors.New("patch child count exceeds mailbox limit")
		}
		out = appendU32(out, uint32(len(op.Children)))
		for _, child := range op.Children {
			out = appendU32(out, uint32(int32(child)))
		}
	}
	if uint64(len(out)) > uint64(MaxMailboxPayload) {
		return nil, fmt.Errorf("patch payload is too large: %d", len(out))
	}
	return out, nil
}

// DecodePatchPayload decodes the patch payload and rejects trailing or
// truncated bytes before the caller touches the DOM.
func DecodePatchPayload(payload []byte) (string, []vm.PatchOp, error) {
	reader := mailboxReader{data: payload}
	islandID, err := reader.string()
	if err != nil {
		return "", nil, err
	}
	count, err := reader.u32()
	if err != nil {
		return "", nil, err
	}
	if count > uint32(len(payload)) {
		return "", nil, errors.New("patch count is impossible for payload size")
	}
	patches := make([]vm.PatchOp, 0, count)
	for i := uint32(0); i < count; i++ {
		kind, err := reader.byte()
		if err != nil {
			return "", nil, err
		}
		values := [4]string{}
		for j := range values {
			values[j], err = reader.string()
			if err != nil {
				return "", nil, err
			}
		}
		childCount, err := reader.u32()
		if err != nil {
			return "", nil, err
		}
		if uint64(childCount) > uint64(reader.remaining()/4) {
			return "", nil, errors.New("patch child count exceeds remaining payload")
		}
		children := make([]int, childCount)
		for j := range children {
			child, err := reader.u32()
			if err != nil {
				return "", nil, err
			}
			children[j] = int(int32(child))
		}
		patches = append(patches, vm.PatchOp{
			Kind:     vm.PatchKind(kind),
			Path:     values[0],
			Tag:      values[1],
			Text:     values[2],
			AttrName: values[3],
			Children: children,
		})
	}
	if reader.remaining() != 0 {
		return "", nil, errors.New("patch payload has trailing bytes")
	}
	return islandID, patches, nil
}

// EncodePatchMailbox wraps a patch payload in a response mailbox.
func EncodePatchMailbox(islandID string, requestID uint32, patches []vm.PatchOp) ([]byte, error) {
	payload, err := EncodePatchPayload(islandID, patches)
	if err != nil {
		return nil, err
	}
	return EncodeMailbox(MailboxOpcodePatches, requestID, MailboxStatusOK, MailboxFlagResponse, payload)
}

// DecodePatchMailbox validates a response mailbox and decodes its patch list.
func DecodePatchMailbox(data []byte) (MailboxHeader, string, []vm.PatchOp, error) {
	header, payload, err := DecodeMailbox(data)
	if err != nil {
		return MailboxHeader{}, "", nil, err
	}
	if header.Opcode != MailboxOpcodePatches || header.Flags&MailboxFlagResponse == 0 {
		return MailboxHeader{}, "", nil, errors.New("mailbox is not a patch response")
	}
	islandID, patches, err := DecodePatchPayload(payload)
	if err != nil {
		return MailboxHeader{}, "", nil, err
	}
	return header, islandID, patches, nil
}

func appendU16(out []byte, value uint16) []byte {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], value)
	return append(out, buf[:]...)
}

func appendU32(out []byte, value uint32) []byte {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], value)
	return append(out, buf[:]...)
}

func appendString(out []byte, value string) ([]byte, error) {
	if uint64(len(value)) > uint64(^uint32(0)) {
		return nil, errors.New("mailbox string exceeds uint32 length")
	}
	out = appendU32(out, uint32(len(value)))
	return append(out, value...), nil
}

type mailboxReader struct {
	data []byte
	pos  int
}

func (r *mailboxReader) take(size int) ([]byte, error) {
	if size < 0 || size > len(r.data)-r.pos {
		return nil, errors.New("mailbox payload is truncated")
	}
	out := r.data[r.pos : r.pos+size]
	r.pos += size
	return out, nil
}

func (r *mailboxReader) byte() (byte, error) {
	data, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

func (r *mailboxReader) u16() (uint16, error) {
	data, err := r.take(2)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(data), nil
}

func (r *mailboxReader) u32() (uint32, error) {
	data, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(data), nil
}

func (r *mailboxReader) string() (string, error) {
	length, err := r.u32()
	if err != nil {
		return "", err
	}
	if uint64(length) > uint64(r.remaining()) {
		return "", errors.New("mailbox string is truncated")
	}
	data, err := r.take(int(length))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (r *mailboxReader) remaining() int {
	return len(r.data) - r.pos
}
