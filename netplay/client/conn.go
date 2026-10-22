package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	hubclient "m31labs.dev/gosx/hub/client"
	"m31labs.dev/gosx/netplay"
	"m31labs.dev/gosx/netplay/dgram"
	"m31labs.dev/gosx/netplay/hubnet"
	"m31labs.dev/gosx/netplay/signal"
)

// Mode says which path a Conn currently uses.
type Mode int32

const (
	// ModeConnecting: neither path is ready yet. Send drops datagrams and Recv
	// returns nothing, like a lossy network; the netplay handshake retries.
	ModeConnecting Mode = iota
	// ModeWebRTC: the unreliable data channel is open. This is the normal path.
	ModeWebRTC
	// ModeWebSocket: WebRTC did not connect (or dropped), so datagrams travel
	// in hub WebSocket frames. This path is reliable and ordered, so loss turns
	// into delay. Apps should tell the player the connection is degraded.
	ModeWebSocket
	// ModeClosed: Close was called.
	ModeClosed
)

func (m Mode) String() string {
	switch m {
	case ModeConnecting:
		return "connecting"
	case ModeWebRTC:
		return "webrtc"
	case ModeWebSocket:
		return "websocket"
	case ModeClosed:
		return "closed"
	}
	return "unknown"
}

// ErrClosed is returned by Send after Close.
var ErrClosed = errors.New("netplay/client: closed")

const (
	defaultFallbackAfter = 4 * time.Second
	defaultConfigWait    = time.Second
	bindingPoll          = 50 * time.Millisecond
	bindingGiveUp        = 30 * time.Second
	queueLen             = 128
)

// Options configures Dial.
type Options struct {
	// HubURL is the hub WebSocket endpoint of the relay, for example
	// "wss://example.com/gosx/hub/match". Dial opens its own connection to it.
	HubURL string
	Header http.Header
	// Session labels this connection in signaling (default "netplay"). Use
	// letters, digits, '.', '_' and '-' only, up to 64 bytes.
	Session string
	// ICEServers are used when the relay sends none. Prefer configuring them
	// on the relay (webrtc.Config.ICEServers), which sends them here.
	ICEServers []signal.ICEServer
	// FallbackAfter is how long to wait for the data channel before using the
	// WebSocket. Zero uses the relay's value, then 4 s.
	FallbackAfter time.Duration
	// ConfigWait is how long to wait for the relay's ICE configuration before
	// starting without it (default 1 s).
	ConfigWait time.Duration
	// OnMode, if set, is called each time Mode changes. It runs on the Conn's
	// own goroutine, never on the game loop, and must return quickly.
	OnMode func(Mode)

	// link makes the browser binding; tests replace it.
	link func() (rtcLink, error)
}

// Conn is a netplay.Transport that uses WebRTC when it can and the hub
// WebSocket when it cannot.
type Conn struct {
	opts Options
	hc   *hubclient.Client

	mode     atomic.Int32
	linkPtr  atomic.Pointer[linkBox]
	linkOpen atomic.Bool
	in       *dgram.Queue

	sendMu sync.Mutex
	frame  [1 + netplay.MaxPacketSize]byte

	events    chan event
	closeCh   chan struct{}
	closeOnce sync.Once
	done      chan struct{}
}

type linkBox struct{ l rtcLink }

type eventKind int

const (
	evHubState  eventKind = iota
	evSignal              // a validated message from the relay
	evOutSignal           // a browser-produced message to send to the relay
	evLink                // link state
)

type event struct {
	kind  eventKind
	state hubclient.State
	msg   signal.Message
	name  string
	data  []byte
}

var _ netplay.Transport = (*Conn)(nil)

// Dial starts connecting and returns at once. The returned Conn is a
// netplay.Transport in ModeConnecting; it switches to WebRTC or the WebSocket
// fallback on its own goroutine.
func Dial(opts Options) (*Conn, error) {
	if opts.HubURL == "" {
		return nil, errors.New("netplay/client: HubURL is required")
	}
	if opts.Session == "" {
		opts.Session = "netplay"
	}
	if err := (&signal.Hello{Session: opts.Session}).Validate(); err != nil {
		return nil, err
	}
	if opts.FallbackAfter < 0 || opts.FallbackAfter > time.Duration(signal.MaxFallbackMs)*time.Millisecond {
		return nil, fmt.Errorf("netplay/client: FallbackAfter must be 0 to %d ms", signal.MaxFallbackMs)
	}
	if opts.ConfigWait == 0 {
		opts.ConfigWait = defaultConfigWait
	}
	if opts.link == nil {
		opts.link = newBrowserLink
	}
	c := &Conn{
		opts: opts, in: dgram.NewQueue(queueLen),
		events: make(chan event, 64), closeCh: make(chan struct{}), done: make(chan struct{}),
	}
	c.frame[0] = hubnet.FrameTag
	c.hc = hubclient.New(hubclient.Options{
		URL: opts.HubURL, Header: opts.Header,
		OnStateChange: func(s hubclient.State) { c.post(event{kind: evHubState, state: s}) },
		OnBinary: func(data []byte) {
			if len(data) > 1 && data[0] == hubnet.FrameTag {
				c.in.Push(data[1:])
			}
		},
	})
	for _, name := range []string{signal.EventConfig, signal.EventAnswer, signal.EventCandidate, signal.EventFailure} {
		name := name
		c.hc.On(name, func(data json.RawMessage) {
			if m, err := signal.Parse(name, data); err == nil {
				c.post(event{kind: evSignal, msg: m})
			}
		})
	}
	c.hc.Connect()
	go c.run()
	return c, nil
}

func (c *Conn) post(e event) {
	select {
	case c.events <- e:
	case <-c.closeCh:
	}
}

// Mode returns the current path.
func (c *Conn) Mode() Mode { return Mode(c.mode.Load()) }

// Degraded reports whether the Conn is on the WebSocket fallback.
func (c *Conn) Degraded() bool { return c.Mode() == ModeWebSocket }

func (c *Conn) setMode(m Mode) {
	if old := Mode(c.mode.Swap(int32(m))); old != m && c.opts.OnMode != nil {
		c.opts.OnMode(m)
	}
}

// Send implements netplay.Transport. A datagram sent before either path is
// ready is dropped, as a lossy network would drop it.
func (c *Conn) Send(p []byte) error {
	if len(p) > netplay.MaxPacketSize {
		return netplay.ErrPacketTooLarge
	}
	switch c.Mode() {
	case ModeClosed:
		return ErrClosed
	case ModeConnecting:
		return nil
	case ModeWebRTC:
		if lb := c.linkPtr.Load(); lb != nil && c.linkOpen.Load() {
			lb.l.Send(p) // a refused datagram is a drop, not an error
			return nil
		}
	}
	c.sendMu.Lock()
	n := 1 + copy(c.frame[1:], p)
	err := c.hc.SendBinary(c.frame[:n])
	c.sendMu.Unlock()
	if errors.Is(err, hubclient.ErrNotConnected) {
		return nil
	}
	if errors.Is(err, hubclient.ErrClosed) {
		return ErrClosed
	}
	return err
}

// Recv implements netplay.Transport. It takes datagrams from both paths, so no
// packet is lost while the Conn switches between them.
func (c *Conn) Recv(buf []byte) (int, bool) {
	if c.Mode() == ModeClosed {
		return 0, false
	}
	if lb := c.linkPtr.Load(); lb != nil {
		if n, ok := lb.l.Recv(buf); ok {
			return n, true
		}
	}
	return c.in.Pop(buf)
}

// Close shuts down both paths.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closeCh)
		<-c.done
	})
	return nil
}

func (c *Conn) run() {
	defer close(c.done)
	defer func() {
		if lb := c.linkPtr.Swap(nil); lb != nil {
			lb.l.Close()
		}
		_ = c.hc.Close()
		c.setMode(ModeClosed)
	}()

	fallbackAfter := c.opts.FallbackAfter
	if fallbackAfter == 0 {
		fallbackAfter = defaultFallbackAfter
	}
	started := time.Now()
	deadline := time.NewTimer(fallbackAfter)
	defer deadline.Stop()
	deadlinePassed := false

	var (
		hubUp       bool
		hello       bool
		linkStarted bool
		linkDead    bool // the link failed or the relay refused WebRTC
		servers     = c.opts.ICEServers
		cfgWait     <-chan time.Time
		pollC       <-chan time.Time
		pollEnd     time.Time
	)
	cfgTimer := time.NewTimer(time.Hour)
	cfgTimer.Stop()
	defer cfgTimer.Stop()
	poll := time.NewTicker(bindingPoll)
	defer poll.Stop()

	evaluate := func() {
		if deadlinePassed && !c.linkOpen.Load() && c.Mode() == ModeConnecting {
			c.setMode(ModeWebSocket)
		}
	}
	degrade := func() {
		c.linkOpen.Store(false)
		if c.Mode() != ModeClosed {
			c.setMode(ModeWebSocket)
		}
	}
	tryStart := func() {
		if linkStarted || linkDead {
			return
		}
		l, err := c.opts.link()
		if err != nil {
			if pollC == nil {
				pollC, pollEnd = poll.C, time.Now().Add(bindingGiveUp)
			}
			return
		}
		pollC = nil
		linkStarted = true
		c.linkPtr.Store(&linkBox{l})
		err = l.Open(rtcConfig{Session: c.opts.Session, ICEServers: servers},
			func(name string, data []byte) {
				c.post(event{kind: evOutSignal, name: name, data: append([]byte(nil), data...)})
			},
			func(state string) { c.post(event{kind: evLink, name: state}) })
		if err != nil {
			c.linkPtr.Store(nil)
			l.Close()
			linkDead = true
			degrade()
		}
	}
	startAfterConfig := func() {
		cfgWait = nil
		cfgTimer.Stop()
		tryStart()
	}

	for {
		select {
		case <-c.closeCh:
			return
		case <-deadline.C:
			deadlinePassed = true
			evaluate()
		case <-cfgWait:
			startAfterConfig()
		case <-pollC:
			if time.Now().After(pollEnd) {
				pollC = nil
				continue
			}
			tryStart()
		case e := <-c.events:
			switch e.kind {
			case evHubState:
				if e.state == hubclient.StateConnected && !hubUp {
					hubUp = true
					if !hello {
						hello = true
						if err := c.hc.Send(signal.EventHello, &signal.Hello{Session: c.opts.Session}); err != nil {
							startAfterConfig()
							break
						}
						cfgTimer.Reset(c.opts.ConfigWait)
						cfgWait = cfgTimer.C
					}
				}
			case evSignal:
				switch m := e.msg.(type) {
				case *signal.Config:
					if m.Session != c.opts.Session || linkStarted {
						break
					}
					if len(m.ICEServers) > 0 {
						servers = m.ICEServers
					}
					if c.opts.FallbackAfter == 0 && m.FallbackAfterMs > 0 {
						d := time.Duration(m.FallbackAfterMs)*time.Millisecond - time.Since(started)
						if d < 0 {
							d = 0
						}
						if !deadline.Stop() {
							select {
							case <-deadline.C:
							default:
							}
						}
						deadline.Reset(d)
					}
					startAfterConfig()
				case *signal.Answer, *signal.Candidate:
					if lb := c.linkPtr.Load(); lb != nil {
						if name, data, err := signal.Marshal(m); err == nil {
							lb.l.Signal(name, data)
						}
					}
				case *signal.Failure:
					if m.Session != c.opts.Session {
						break
					}
					linkDead = true
					if lb := c.linkPtr.Swap(nil); lb != nil {
						lb.l.Close()
					}
					degrade()
				}
			case evOutSignal:
				if _, err := signal.Parse(e.name, e.data); err == nil {
					_ = c.hc.Send(e.name, json.RawMessage(e.data))
				}
			case evLink:
				switch e.name {
				case linkOpen:
					c.linkOpen.Store(true)
					c.setMode(ModeWebRTC)
				case linkFailed, linkClosed:
					if e.name == linkFailed {
						linkDead = true
						if lb := c.linkPtr.Swap(nil); lb != nil {
							lb.l.Close()
						}
					}
					degrade()
				}
			}
			evaluate()
		}
	}
}
