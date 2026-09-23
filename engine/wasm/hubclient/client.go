// Package hubclient gives Go-WASM engines a typed client for GoSX hub wire
// messages. The browser transport is owned by the GoSX host runtime.
package hubclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const DefaultMaxFrameBytes = 8 << 20

var (
	ErrUnavailable   = errors.New("gosx WASM hub transport is unavailable")
	ErrNotOpen       = errors.New("gosx hub socket is not open")
	ErrDisposed      = errors.New("gosx hub client is disposed")
	ErrFrameTooLarge = errors.New("gosx hub frame exceeds size limit")
)

type State uint8

const (
	Connecting State = iota
	Open
	Closing
	Closed
)

// Handler receives one decoded hub envelope per text frame. Data remains raw
// JSON so a game can decode only the message types it uses.
type Handler struct {
	OnOpen    func()
	OnMessage func(event string, data json.RawMessage)
	OnClose   func(code int, reason string)
	OnError   func(error)
}

type Options struct{ MaxFrameBytes int }

type wireMessage struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
}

func decodeEnvelope(raw []byte) (string, json.RawMessage, error) {
	var message wireMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(message.Event) == "" {
		return "", nil, errors.New("hub event is empty")
	}
	return message.Event, message.Data, nil
}

func encodeEnvelope(event string, data any) ([]byte, error) {
	if strings.TrimSpace(event) == "" {
		return nil, errors.New("hub event is empty")
	}
	encoded, err := json.Marshal(struct {
		Event string `json:"event"`
		Data  any    `json:"data"`
	}{Event: event, Data: data})
	if err != nil {
		return nil, fmt.Errorf("encode hub message: %w", err)
	}
	return encoded, nil
}

func frameLimit(options Options) int {
	if options.MaxFrameBytes <= 0 || options.MaxFrameBytes > DefaultMaxFrameBytes {
		return DefaultMaxFrameBytes
	}
	return options.MaxFrameBytes
}
