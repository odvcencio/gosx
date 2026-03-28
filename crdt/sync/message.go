package sync

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const MessageTypeV1 = 0x42

// Message is the wire-level sync envelope.
type Message struct {
	Version byte
	Heads   [][32]byte
	Need    [][32]byte
	Changes [][]byte
}

type messageEnvelope struct {
	Version byte     `json:"version"`
	Heads   []string `json:"heads,omitempty"`
	Need    []string `json:"need,omitempty"`
	Changes [][]byte `json:"changes,omitempty"`
}

func EncodeMessage(message Message) ([]byte, error) {
	env := messageEnvelope{
		Version: message.Version,
		Heads:   encodeHashes(message.Heads),
		Need:    encodeHashes(message.Need),
		Changes: message.Changes,
	}
	body, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return append([]byte{message.Version}, body...), nil
}

func DecodeMessage(data []byte) (Message, error) {
	if len(data) == 0 {
		return Message{}, fmt.Errorf("empty sync message")
	}
	var env messageEnvelope
	if err := json.Unmarshal(data[1:], &env); err != nil {
		return Message{}, fmt.Errorf("decode sync envelope: %w", err)
	}
	heads, err := decodeHashes(env.Heads)
	if err != nil {
		return Message{}, err
	}
	need, err := decodeHashes(env.Need)
	if err != nil {
		return Message{}, err
	}
	version := env.Version
	if version == 0 {
		version = data[0]
	}
	return Message{
		Version: version,
		Heads:   heads,
		Need:    need,
		Changes: env.Changes,
	}, nil
}

func encodeHashes(values [][32]byte) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, hex.EncodeToString(value[:]))
	}
	return out
}

func decodeHashes(values []string) ([][32]byte, error) {
	out := make([][32]byte, 0, len(values))
	for _, value := range values {
		raw, err := hex.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("decode hash %q: %w", value, err)
		}
		if len(raw) != 32 {
			return nil, fmt.Errorf("invalid hash length for %q", value)
		}
		var hash [32]byte
		copy(hash[:], raw)
		out = append(out, hash)
	}
	return out, nil
}
