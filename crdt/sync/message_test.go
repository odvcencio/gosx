package sync

import "testing"

func TestMessageRoundTripPreservesNeedAndChanges(t *testing.T) {
	message := Message{
		Version: MessageTypeV1,
		Heads: [][32]byte{
			hashByte(1),
			hashByte(2),
		},
		Need: [][32]byte{
			hashByte(3),
		},
		Changes: [][]byte{
			[]byte("change-a"),
			[]byte("change-b"),
		},
	}

	encoded, err := EncodeMessage(message)
	if err != nil {
		t.Fatalf("encode message: %v", err)
	}
	decoded, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("decode message: %v", err)
	}

	if decoded.Version != message.Version {
		t.Fatalf("expected version %d, got %d", message.Version, decoded.Version)
	}
	if len(decoded.Heads) != len(message.Heads) {
		t.Fatalf("expected %d heads, got %d", len(message.Heads), len(decoded.Heads))
	}
	if len(decoded.Need) != len(message.Need) {
		t.Fatalf("expected %d need hashes, got %d", len(message.Need), len(decoded.Need))
	}
	if len(decoded.Changes) != len(message.Changes) {
		t.Fatalf("expected %d changes, got %d", len(message.Changes), len(decoded.Changes))
	}
	if string(decoded.Changes[0]) != "change-a" || string(decoded.Changes[1]) != "change-b" {
		t.Fatalf("unexpected decoded changes %#v", decoded.Changes)
	}
}

func hashByte(value byte) [32]byte {
	var hash [32]byte
	hash[0] = value
	return hash
}
