//go:build js && wasm

package hubclient

import (
	"errors"
	"fmt"
	"strings"
	"syscall/js"
)

type Client struct {
	handler    Handler
	limit      int
	handle     js.Value
	callback   js.Func
	generation uint64
	disposed   bool
}

// Dial opens one socket. Reconnect timing and session policy belong to the
// caller; the framework never retries or replays an input on its own.
func Dial(path string, options Options, handler Handler) (*Client, error) {
	c := &Client{handler: handler, limit: frameLimit(options)}
	if err := c.Reconnect(path); err != nil {
		return nil, err
	}
	return c, nil
}

// Reconnect replaces this client's socket and discards callbacks from its old
// transport generation. The caller chooses when and with which resume URL.
func (c *Client) Reconnect(path string) (err error) {
	if c == nil || c.disposed {
		return ErrDisposed
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("hub path is empty")
	}
	namespace := js.Global().Get("__gosx")
	if !namespace.Truthy() {
		return ErrUnavailable
	}
	host := namespace.Get("host")
	if !host.Truthy() {
		return ErrUnavailable
	}
	api := host.Get("wasmHub")
	if !api.Truthy() || api.Get("open").Type() != js.TypeFunction {
		return ErrUnavailable
	}
	c.releaseTransport()
	c.generation++
	generation := c.generation
	c.callback = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if c.disposed || generation != c.generation || len(args) < 4 {
			return nil
		}
		switch args[0].String() {
		case "open":
			if c.handler.OnOpen != nil {
				c.handler.OnOpen()
			}
		case "message":
			raw := args[1].String()
			if len(raw) > c.limit {
				c.report(ErrFrameTooLarge)
				c.Close(4009, "frame too large")
				return nil
			}
			event, data, decodeErr := decodeEnvelope([]byte(raw))
			if decodeErr != nil {
				c.report(decodeErr)
				return nil
			}
			if c.handler.OnMessage != nil {
				c.handler.OnMessage(event, data)
			}
		case "close":
			if c.handler.OnClose != nil {
				c.handler.OnClose(args[2].Int(), args[3].String())
			}
		case "error":
			if args[1].String() == "frame-too-large" {
				c.report(ErrFrameTooLarge)
			} else {
				c.report(errors.New("hub socket error"))
			}
		}
		return nil
	})
	defer func() {
		if recovered := recover(); recovered != nil {
			c.releaseTransport()
			err = fmt.Errorf("open hub socket: %v", recovered)
		}
	}()
	c.handle = api.Call("open", path, c.limit, c.callback)
	if !c.handle.Truthy() {
		c.releaseTransport()
		return ErrUnavailable
	}
	return nil
}

func (c *Client) report(err error) {
	if c.handler.OnError != nil {
		c.handler.OnError(err)
	}
}

func (c *Client) Send(event string, data any) error {
	if c == nil || c.disposed {
		return ErrDisposed
	}
	encoded, err := encodeEnvelope(event, data)
	if err != nil {
		return err
	}
	if len(encoded) > c.limit {
		return ErrFrameTooLarge
	}
	if c.ReadyState() != Open || !c.handle.Call("send", string(encoded)).Bool() {
		return ErrNotOpen
	}
	return nil
}

func (c *Client) ReadyState() State {
	if c == nil || c.disposed || !c.handle.Truthy() {
		return Closed
	}
	state := c.handle.Call("state").Int()
	if state < 0 || state > 3 {
		return Closed
	}
	return State(state)
}

func (c *Client) Close(code int, reason string) {
	if c == nil || c.disposed || !c.handle.Truthy() {
		return
	}
	c.handle.Call("close", code, reason)
}

func (c *Client) releaseTransport() {
	if c.handle.Truthy() {
		c.handle.Call("dispose")
	}
	c.handle = js.Undefined()
	if c.callback.Value.Truthy() {
		c.callback.Release()
	}
	c.callback = js.Func{}
}

func (c *Client) Dispose() {
	if c == nil || c.disposed {
		return
	}
	c.disposed = true
	c.generation++
	c.releaseTransport()
}
