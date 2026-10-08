package hubclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
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
	// is called from the client's internal goroutine. It may call Close;
	// Close then requests shutdown without waiting for this callback to return.
	// Other blocking work delays the reconnect loop.
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

	mu            sync.Mutex
	state         State
	resumeToken   string
	handlers      map[string]func(json.RawMessage)
	current       conn
	started       bool
	closed        bool
	closeCh       chan struct{}
	done          chan struct{}
	loopID        uint64
	callbackDepth int
	dialCtx       context.Context
	cancelDial    context.CancelFunc
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
	dialCtx, cancelDial := context.WithCancel(context.Background())
	c := &Client{
		opts:       opts,
		dial:       newDefaultDialer(opts.EnableCompression),
		backoff:    opts.Backoff,
		handlers:   make(map[string]func(json.RawMessage)),
		closeCh:    make(chan struct{}),
		done:       make(chan struct{}),
		dialCtx:    dialCtx,
		cancelDial: cancelDial,
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
// like "session". Handlers run on the reconnect loop's goroutine and may call
// Close, which requests shutdown without waiting for the handler to return.
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
// Like Client.On handlers, handler may call Close without waiting for shutdown.
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
	connected := c.state == StateConnected
	c.mu.Unlock()
	if cn == nil || !connected {
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
	if c.closed || c.started {
		c.mu.Unlock()
		return
	}
	c.started = true
	c.mu.Unlock()
	go c.run()
}

// Close stops the reconnect loop and closes any pending or open connection.
// Calls from On handlers or Options.OnStateChange request shutdown without
// waiting for the callback's own goroutine. All other calls, including concurrent
// calls, wait until the loop finishes and StateClosed callbacks return.
// If goroutine identification is unavailable, Close skips waiting while any
// callback is running because it cannot distinguish callback calls.
// Under a heavy server broadcast flood, the server may record write_error rather
// than peer_closed because the client stops reading before closing.
// A closed Client cannot be reused; construct a new one with New.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.closeCh)
		c.cancelDial()
		if !c.started {
			// Use the same finalizer even when Connect was never called, so a
			// StateClosed callback can itself call Close safely.
			c.started = true
			go c.run()
		}
	}
	cn := c.current
	loopID := c.loopID
	inCallback := c.callbackDepth > 0
	c.mu.Unlock()
	if cn != nil {
		_ = cn.Close()
	}
	callerID := currentGoroutineID()
	if loopID != 0 && callerID != 0 {
		if callerID == loopID {
			return nil
		}
	} else if inCallback {
		return nil
	}
	<-c.done
	return nil
}

func (c *Client) run() {
	c.mu.Lock()
	c.loopID = currentGoroutineID()
	c.mu.Unlock()
	defer func() {
		c.setState(StateClosed)
		close(c.done)
	}()
	attempt := 0
	for {
		if c.isClosed() {
			return
		}
		c.setState(StateConnecting)
		cn, err := c.dial.Dial(c.dialCtx, c.dialURL(), c.opts.Header)
		if err != nil {
			attempt++
			if !c.waitRetry(attempt) {
				return
			}
			continue
		}
		// Own the transport as soon as Dial returns. Browser sockets can
		// remain connecting until a later frameOpen, and native Dial may
		// finish after Close has already stopped the client.
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			_ = cn.Close()
			return
		}
		c.current = cn
		c.mu.Unlock()
		opened := c.pump(cn)
		_ = cn.Close()
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

// currentGoroutineID identifies reentrant Close calls without changing callback
// ordering or making an unrelated caller skip its shutdown wait. Go exposes no
// goroutine ID API; runtime.Stack's first line supplies it on native and wasm.
// Missing or unrecognized headers return zero for the callback fallback.
// Keep this dependency here, outside message dispatch's hot path.
func currentGoroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	id, _ := parseGoroutineID(buf[:n])
	return id
}

func parseGoroutineID(header []byte) (uint64, bool) {
	const prefix = "goroutine "
	if !bytes.HasPrefix(header, []byte(prefix)) {
		return 0, false
	}
	end := len(prefix)
	for end < len(header) && header[end] >= '0' && header[end] <= '9' {
		end++
	}
	if end == len(prefix) {
		return 0, false
	}
	id, err := strconv.ParseUint(string(header[len(prefix):end]), 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// pump consumes cn's event channel until it or the client closes, dispatching
// messages and tracking state. It returns whether the connection ever opened.
func (c *Client) pump(cn conn) bool {
	opened := false
	for {
		var ev frameEvent
		select {
		case <-c.closeCh:
			return opened
		case next, ok := <-cn.Events():
			if !ok || c.isClosed() {
				return opened
			}
			ev = next
		}
		switch ev.Kind {
		case frameOpen:
			opened = true
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
}

func (c *Client) dispatch(data []byte) {
	var msg wireMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	handler := c.handlers[msg.Event]
	c.mu.Unlock()
	if handler != nil {
		c.callCallback(func() { handler(msg.Data) })
	}
}

// callCallback tracks callback execution for runtimes that cannot report a
// goroutine ID. The normal Close path still uses identity to make external
// callers wait even when a callback is running.
func (c *Client) callCallback(fn func()) {
	c.mu.Lock()
	c.callbackDepth++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.callbackDepth--
		c.mu.Unlock()
	}()
	fn()
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
	if c.closed && state != StateClosed {
		c.mu.Unlock()
		return
	}
	changed := c.state != state
	c.state = state
	fn := c.opts.OnStateChange
	c.mu.Unlock()
	if changed && fn != nil {
		c.callCallback(func() { fn(state) })
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
