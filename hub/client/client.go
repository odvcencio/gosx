package hubclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// ErrNotConnected is returned by Send when no transport is currently open.
var ErrNotConnected = errors.New("hubclient: not connected")

// ErrClosed is returned by Send and Connect once Close has been called.
var ErrClosed = errors.New("hubclient: closed")

const defaultResumeParam = "resume"
const defaultStoreKey = "hubclient.resume"

// Options configures a Client.
type Options struct {
	// URL is the hub WebSocket endpoint, for example
	// "wss://example.com/gosx/hub/room-42". Required.
	URL string
	// Header carries additional request headers for the native (gorilla)
	// transport. The browser transport ignores it — the WebSocket API does
	// not allow custom request headers.
	Header http.Header
	// Backoff controls reconnect timing. The zero value is DefaultBackoff.
	Backoff Backoff
	// ResumeParam is the URL query parameter that carries a non-empty resume
	// token on every dial attempt. Empty selects "resume". This matches the
	// convention an application's HTTP handler reads before calling
	// hub.ServeHTTPWithMetadata to build connection metadata — see package
	// doc.
	ResumeParam string
	// ResumeToken seeds the initial resume token when Store is nil, or when
	// Store has no value yet for StoreKey.
	ResumeToken string
	// Store, if set, persists the resume token across reconnects and page
	// reloads. game/storage.Namespaced satisfies this interface.
	Store TokenStore
	// StoreKey is the key used with Store. Empty selects "hubclient.resume".
	StoreKey string
	// OnStateChange, if set, is called on every Client.State transition. It
	// is called from the client's internal goroutine — treat it like an
	// event handler, not call back into blocking work.
	OnStateChange func(State)
	// EnableCompression offers permessage-deflate at handshake on the native
	// (gorilla) transport, matching a hub's own EnableCompression opt-in.
	// Off by default. The browser transport ignores this field: every major
	// browser already offers permessage-deflate on its own, so there is
	// nothing to enable from client Go code running as js/wasm.
	EnableCompression bool
}

// Client is a reconnecting hub connection. The zero value is not usable;
// construct one with New.
type Client struct {
	opts    Options
	dial    dialer
	backoff Backoff

	mu          sync.Mutex
	state       State
	resumeToken string
	handlers    map[string]func(json.RawMessage)
	current     conn
	closed      bool
	closeCh     chan struct{}
	wg          sync.WaitGroup
}

// New creates a Client for opts. It does not dial — call Connect to start
// the reconnect loop.
func New(opts Options) *Client {
	if opts.ResumeParam == "" {
		opts.ResumeParam = defaultResumeParam
	}
	if opts.StoreKey == "" {
		opts.StoreKey = defaultStoreKey
	}
	c := &Client{
		opts:     opts,
		dial:     newDefaultDialer(opts.EnableCompression),
		backoff:  opts.Backoff,
		handlers: make(map[string]func(json.RawMessage)),
		closeCh:  make(chan struct{}),
	}
	c.resumeToken = opts.ResumeToken
	if opts.Store != nil {
		if v, ok := opts.Store.Get(opts.StoreKey); ok && v != "" {
			c.resumeToken = v
		}
	}
	return c
}

// On registers handler for event, replacing any previously registered
// handler for the same event name. Event names mirror what the server
// registers with hub.On — for example "sim:tick" or a game-specific event
// like "session".
func (c *Client) On(event string, handler func(data json.RawMessage)) {
	if c == nil || event == "" || handler == nil {
		return
	}
	c.mu.Lock()
	c.handlers[event] = handler
	c.mu.Unlock()
}

// On registers a typed handler for event: data is JSON-decoded into T before
// handler runs. A decode failure is silently dropped, matching hub's own
// handler dispatch, which never surfaces a decode error to the caller.
func On[T any](c *Client, event string, handler func(value T)) {
	if c == nil || handler == nil {
		return
	}
	c.On(event, func(data json.RawMessage) {
		var value T
		if err := json.Unmarshal(data, &value); err != nil {
			return
		}
		handler(value)
	})
}

// State returns the client's current connection state.
func (c *Client) State() State {
	if c == nil {
		return StateDisconnected
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// ResumeToken returns the current resume token, which may be empty.
func (c *Client) ResumeToken() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resumeToken
}

// SetResumeToken updates the resume token used on future dial attempts and
// persists it to Options.Store, if one was configured. Call this from a
// handler for whatever event the server uses to hand out or renew a resume
// token — the wire protocol for that event is application-defined; hubclient
// only carries the token, it does not assign one.
func (c *Client) SetResumeToken(token string) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.resumeToken = token
	store := c.opts.Store
	key := c.opts.StoreKey
	c.mu.Unlock()
	if store == nil {
		return nil
	}
	return store.Set(key, token)
}

// Send encodes data as JSON and writes a {"event","data"} envelope to the
// current connection. It returns ErrNotConnected if no transport is
// currently open, and ErrClosed once Close has been called. A caller that
// wants at-least-once delivery across a reconnect must retry Send itself —
// hubclient does not queue outbound messages.
func (c *Client) Send(event string, data any) error {
	if c == nil {
		return ErrClosed
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	msg, err := json.Marshal(wireMessage{Event: event, Data: payload})
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	cn := c.current
	c.mu.Unlock()
	if cn == nil {
		return ErrNotConnected
	}
	return cn.Send(msg, false)
}

// Connect starts the reconnect loop in a background goroutine and returns
// immediately. Calling Connect more than once, or after Close, is a no-op.
func (c *Client) Connect() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.closed || c.state != StateDisconnected {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	c.wg.Add(1)
	go c.run()
}

// Close stops the reconnect loop and closes any open connection. It blocks
// until the background goroutine has exited. A closed Client cannot be
// reused; construct a new one with New.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	cn := c.current
	close(c.closeCh)
	c.mu.Unlock()
	if cn != nil {
		_ = cn.Close()
	}
	c.wg.Wait()
	c.setState(StateClosed)
	return nil
}

func (c *Client) run() {
	defer c.wg.Done()
	attempt := 0
	for {
		if c.isClosed() {
			return
		}
		c.setState(StateConnecting)
		cn, err := c.dial.Dial(c.dialURL(), c.opts.Header)
		if err != nil {
			attempt++
			if !c.waitRetry(attempt) {
				return
			}
			continue
		}
		opened := c.pump(cn)
		c.mu.Lock()
		c.current = nil
		c.mu.Unlock()
		if c.isClosed() {
			return
		}
		if opened {
			attempt = 0
		} else {
			attempt++
		}
		if !c.waitRetry(attempt) {
			return
		}
	}
}

// pump consumes cn's event channel until it closes, dispatching messages and
// tracking connection state. It returns whether the connection ever reached
// the open state.
func (c *Client) pump(cn conn) bool {
	opened := false
	for ev := range cn.Events() {
		switch ev.Kind {
		case frameOpen:
			opened = true
			c.mu.Lock()
			c.current = cn
			c.mu.Unlock()
			c.setState(StateConnected)
		case frameMessage:
			c.dispatch(ev.Data)
		case frameError:
			// A frameClosed always follows; nothing to do here beyond letting
			// the loop continue draining events.
		case frameClosed:
			// Keep draining in case the transport still has a final event
			// queued, but there normally isn't one after frameClosed.
		}
	}
	return opened
}

func (c *Client) dispatch(data []byte) {
	var msg wireMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	c.mu.Lock()
	handler := c.handlers[msg.Event]
	c.mu.Unlock()
	if handler != nil {
		handler(msg.Data)
	}
}

// waitRetry sets StateReconnecting and blocks for the backoff delay for
// attempt, returning false if the client was closed while waiting.
func (c *Client) waitRetry(attempt int) bool {
	if c.isClosed() {
		return false
	}
	c.setState(StateReconnecting)
	delay := c.backoff.Delay(attempt)
	if delay <= 0 {
		return !c.isClosed()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return !c.isClosed()
	case <-c.closeCh:
		return false
	}
}

func (c *Client) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *Client) setState(state State) {
	c.mu.Lock()
	changed := c.state != state
	c.state = state
	fn := c.opts.OnStateChange
	c.mu.Unlock()
	if changed && fn != nil {
		fn(state)
	}
}

func (c *Client) dialURL() string {
	c.mu.Lock()
	token := c.resumeToken
	c.mu.Unlock()
	if token == "" {
		return c.opts.URL
	}
	parsed, err := url.Parse(c.opts.URL)
	if err != nil {
		// Fall back to naive concatenation so a malformed base URL still
		// fails at Dial with a clear error instead of silently dropping the
		// resume token.
		sep := "?"
		if containsQuery(c.opts.URL) {
			sep = "&"
		}
		return fmt.Sprintf("%s%s%s=%s", c.opts.URL, sep, c.opts.ResumeParam, url.QueryEscape(token))
	}
	q := parsed.Query()
	q.Set(c.opts.ResumeParam, token)
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

func containsQuery(raw string) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] == '?' {
			return true
		}
	}
	return false
}
