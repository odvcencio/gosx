package hosttest

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"m31labs.dev/gosx/game/host"
)

// Errors a caller can test with errors.Is or errors.As.
var (
	// ErrClosed means Close ran, or the Start context ended.
	ErrClosed = errors.New("hosttest: server closed")
	// ErrPageGone means the page's connection ended.
	ErrPageGone = errors.New("hosttest: page connection lost")
	// ErrNotConnected means no page has connected yet.
	ErrNotConnected = errors.New("hosttest: no page connected")
	// ErrNotPlugged means the operation names an empty slot.
	ErrNotPlugged = errors.New("hosttest: pad slot is empty")
	// ErrAlreadyPlugged means Plug named an occupied slot. Unplug it first.
	ErrAlreadyPlugged = errors.New("hosttest: pad slot is occupied")
	// ErrFrameNotSampled means SetAt latched on its frame but no read happened
	// on that frame, so the receipt names a later frame.
	ErrFrameNotSampled = errors.New("hosttest: exact frame passed with no pad read")
)

// LateFrameError means the page rejected an exact-frame change because that
// frame had begun.
type LateFrameError struct {
	Seq       uint64
	Requested uint64
	Current   uint64
}

func (e *LateFrameError) Error() string {
	return fmt.Sprintf("hosttest: change %d for frame %d is late: the page is at frame %d", e.Seq, e.Requested, e.Current)
}

// RejectedError means the page refused a change for another reason.
type RejectedError struct {
	Seq    uint64
	Reason string
	Frame  uint64
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("hosttest: page rejected change %d (%s) at frame %d", e.Seq, e.Reason, e.Frame)
}

// Receipt says what the page had done when an operation finished.
type Receipt struct {
	// Seq is the change's sequence number (the latest applied, for progress).
	Seq uint64
	// Frame is the frame whose end carried the receipt: the first frame that
	// read the change.
	Frame uint64
	// Drawn counts completed frames and Sampled counts completed frames that
	// read the pads, both as of that frame's end.
	Drawn   uint64
	Sampled uint64
}

// Options tunes Start. Zero values select the defaults.
type Options struct {
	// HelloTimeout is how long a new connection has to send a valid hello.
	// Default 60 seconds: a page on a loaded machine can take many seconds
	// to reach the point where it says hello.
	HelloTimeout time.Duration
	// MaxMessage is the largest message accepted. Default host.PadTestMaxMessage.
	MaxMessage int64

	// dropFirstHello makes the server close the first valid connection
	// without answering its hello, to test the page's retry.
	dropFirstHello bool
}

// defaultHelloTimeout is the hello timeout Options uses when it sets none.
const defaultHelloTimeout = 60 * time.Second

const maxUnauthenticated = 8

// Server drives one page's pads over a loopback WebSocket. It is single use:
// after the page's connection ends, or after Close, it refuses everything.
// Mutating methods run one at a time and each waits for the page's receipt.
// All methods are safe for concurrent use.
type Server struct {
	opts  Options
	token string
	port  int
	ln    net.Listener
	srv   *http.Server

	mut sync.Mutex // serializes mutators: one unacknowledged change at a time
	wmu sync.Mutex // serializes socket writes

	mu       sync.Mutex
	unauth   int
	claimed  bool
	dropped  bool // dropFirstHello has run
	conn     *websocket.Conn
	done     chan struct{}
	doneErr  error
	ready    chan struct{}
	readyOK  bool
	drawn    uint64
	sampled  uint64
	frame    uint64
	applied  uint64 // latest applied sequence
	sentSeq  uint64
	nextSeq  uint64
	nextWait uint64
	desired  map[int]host.GamepadState
	applies  map[uint64]*applyWaiter
	watches  map[uint64]*watchWaiter
}

type applyResult struct {
	rcpt Receipt
	err  error
}

type applyWaiter struct {
	frame uint64 // requested exact frame, 0 for next
	ch    chan applyResult
}

type watchWaiter struct {
	absolute       bool
	drawn, sampled uint64
	ch             chan applyResult
}

// Start listens on 127.0.0.1 with an OS-chosen port and a fresh random token.
// The server ends when ctx ends.
func Start(ctx context.Context) (*Server, error) { return StartWith(ctx, Options{}) }

// StartWith is Start with Options.
func StartWith(ctx context.Context, opts Options) (*Server, error) {
	if opts.HelloTimeout <= 0 {
		opts.HelloTimeout = defaultHelloTimeout
	}
	if opts.MaxMessage <= 0 {
		opts.MaxMessage = host.PadTestMaxMessage
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{
		opts: opts, token: hex.EncodeToString(raw), ln: ln,
		port:    ln.Addr().(*net.TCPAddr).Port,
		done:    make(chan struct{}),
		ready:   make(chan struct{}),
		desired: map[int]host.GamepadState{},
		applies: map[uint64]*applyWaiter{},
		watches: map[uint64]*watchWaiter{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc(host.PadTestPath, s.handle)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: opts.HelloTimeout}
	go s.srv.Serve(ln)
	go func() {
		select {
		case <-ctx.Done():
			s.fail(ErrClosed)
		case <-s.done:
		}
	}()
	return s, nil
}

// PageURL returns page with one query field added:
//
//	gosx-padtest=<port>.<token>
//
// The port is the listener's loopback port and the token is 32 lowercase hex
// characters, so the value needs no escaping. Any existing gosx-padtest field
// is replaced; other query fields and the fragment are kept as they were.
func (s *Server) PageURL(page url.URL) url.URL {
	var keep []string
	if page.RawQuery != "" {
		for _, part := range strings.Split(page.RawQuery, "&") {
			if part != "" && !strings.HasPrefix(part, host.PadTestQueryField+"=") && part != host.PadTestQueryField {
				keep = append(keep, part)
			}
		}
	}
	keep = append(keep, host.PadTestQueryField+"="+host.FormatPadTestEndpoint(s.port, s.token))
	page.RawQuery = strings.Join(keep, "&")
	return page
}

// Addr returns the listener address, "127.0.0.1:<port>".
func (s *Server) Addr() string { return s.ln.Addr().String() }

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	refusal := ""
	switch {
	case s.isDoneLocked():
		refusal = "server finished"
	case s.claimed:
		refusal = "page already connected"
	}
	if refusal != "" {
		s.mu.Unlock()
		// Upgrade and then close with a final code and a reason. An HTTP
		// status before the upgrade reaches a browser page as an error event
		// and a 1006 close, which the page reads as "try again".
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, refusal), time.Now().Add(time.Second))
		conn.Close()
		return
	}
	if s.unauth >= maxUnauthenticated {
		s.mu.Unlock()
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	s.unauth++
	s.mu.Unlock()
	counted := true
	uncount := func() {
		if counted {
			counted = false
			s.mu.Lock()
			s.unauth--
			s.mu.Unlock()
		}
	}
	defer uncount()

	// The token is the authentication. A file page has an opaque origin, so
	// the Origin header proves nothing and is not checked.
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(s.opts.MaxMessage)
	conn.SetReadDeadline(time.Now().Add(s.opts.HelloTimeout))
	refuse := func(code int, text string) {
		conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, text), time.Now().Add(time.Second))
		conn.Close()
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}
	hello, err := host.DecodePadMessage(data)
	if err != nil || hello.Type != host.PadMsgHello || hello.Version != host.PadTestProtocolVersion ||
		subtle.ConstantTimeCompare([]byte(hello.Token), []byte(s.token)) != 1 {
		refuse(websocket.ClosePolicyViolation, "bad hello")
		return
	}
	s.mu.Lock()
	if s.claimed || s.isDoneLocked() {
		s.mu.Unlock()
		refuse(websocket.ClosePolicyViolation, "page already connected")
		return
	}
	if s.opts.dropFirstHello && !s.dropped {
		s.dropped = true
		s.mu.Unlock()
		conn.Close()
		return
	}
	s.claimed = true
	s.conn = conn
	s.mu.Unlock()
	uncount()
	conn.SetReadDeadline(time.Time{})
	if err := s.write(conn, host.PadMessage{Type: host.PadMsgReady, Version: host.PadTestProtocolVersion}); err != nil {
		s.fail(fmt.Errorf("%w: %v", ErrPageGone, err))
		return
	}
	s.readLoop(conn)
}

func (s *Server) readLoop(conn *websocket.Conn) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			s.fail(fmt.Errorf("%w: %v", ErrPageGone, err))
			return
		}
		m, err := host.DecodePadMessage(data)
		if err != nil {
			s.fail(fmt.Errorf("%w: %v", ErrPageGone, err))
			return
		}
		s.mu.Lock()
		switch m.Type {
		case host.PadMsgApplied:
			s.note(m)
			if w, ok := s.applies[m.Seq]; ok {
				delete(s.applies, m.Seq)
				w.ch <- applyResult{rcpt: Receipt{Seq: m.Seq, Frame: m.Frame, Drawn: m.Drawn, Sampled: m.Sampled}}
			}
		case host.PadMsgRejected:
			if w, ok := s.applies[m.Seq]; ok {
				delete(s.applies, m.Seq)
				var err error = &RejectedError{Seq: m.Seq, Reason: m.Reason, Frame: m.Frame}
				if m.Reason == host.PadRejectLate {
					err = &LateFrameError{Seq: m.Seq, Requested: w.frame, Current: m.Frame}
				}
				w.ch <- applyResult{err: err}
			}
		case host.PadMsgProgress:
			s.note(m)
			if w, ok := s.watches[m.Watch]; ok && m.Watch != 0 {
				delete(s.watches, m.Watch)
				w.ch <- applyResult{rcpt: s.receiptLocked()}
			}
		default:
			s.mu.Unlock()
			s.fail(fmt.Errorf("%w: unexpected %q message", ErrPageGone, m.Type))
			return
		}
		s.mu.Unlock()
	}
}

// note folds a page message's counters into the cache and wakes any waiter on
// an absolute threshold they now meet. Callers hold s.mu.
func (s *Server) note(m host.PadMessage) {
	if m.Drawn > s.drawn {
		s.drawn = m.Drawn
	}
	if m.Sampled > s.sampled {
		s.sampled = m.Sampled
	}
	if m.Frame > s.frame {
		s.frame = m.Frame
	}
	if m.Type == host.PadMsgApplied && m.Seq > s.applied {
		s.applied = m.Seq
	}
	if m.Type == host.PadMsgProgress && m.Seq > s.applied {
		s.applied = m.Seq
	}
	if !s.readyOK && s.sampled >= 1 {
		s.readyOK = true
		close(s.ready)
	}
	for id, w := range s.watches {
		if w.absolute && s.reachedLocked(w.drawn, w.sampled) {
			delete(s.watches, id)
			w.ch <- applyResult{rcpt: s.receiptLocked()}
		}
	}
}

func (s *Server) reachedLocked(drawn, sampled uint64) bool {
	return s.drawn >= drawn && s.sampled >= sampled
}

func (s *Server) receiptLocked() Receipt {
	return Receipt{Seq: s.applied, Frame: s.frame, Drawn: s.drawn, Sampled: s.sampled}
}

func (s *Server) isDoneLocked() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// fail shuts the server down once: it closes the listener and the page, and
// fails every pending operation with err.
func (s *Server) fail(err error) {
	s.mu.Lock()
	if s.isDoneLocked() {
		s.mu.Unlock()
		return
	}
	s.doneErr = err
	close(s.done)
	conn := s.conn
	for seq, w := range s.applies {
		delete(s.applies, seq)
		w.ch <- applyResult{err: err}
	}
	for id, w := range s.watches {
		delete(s.watches, id)
		w.ch <- applyResult{err: err}
	}
	s.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
	s.srv.Close()
	s.ln.Close()
}

func (s *Server) write(conn *websocket.Conn, m host.PadMessage) error {
	data, err := host.EncodePadMessage(m)
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteMessage(websocket.TextMessage, data)
}

// live returns the page connection or the reason there is none. Callers hold s.mu.
func (s *Server) liveLocked() (*websocket.Conn, error) {
	if s.isDoneLocked() {
		return nil, s.doneErr
	}
	if s.conn == nil {
		return nil, ErrNotConnected
	}
	return s.conn, nil
}

// diag describes where the page was, for failure messages.
func (s *Server) diag() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := "open"
	if s.conn == nil {
		state = "never connected"
	}
	if s.isDoneLocked() {
		state = "closed: " + s.doneErr.Error()
	}
	return fmt.Sprintf("last drawn %d, sampled %d, frame %d, sent seq %d, applied seq %d, connection %s",
		s.drawn, s.sampled, s.frame, s.sentSeq, s.applied, state)
}

func (s *Server) wrap(op string, err error) error {
	return fmt.Errorf("hosttest: %s: %w (%s)", op, err, s.diag())
}

// Close ends the server and the page connection. Pending operations fail with
// ErrClosed. It is safe to call more than once.
func (s *Server) Close() error {
	s.fail(ErrClosed)
	return nil
}

// WaitReady waits for the page's hello and for its first frame that read the
// pads. A socket that opened is not enough.
func (s *Server) WaitReady(ctx context.Context) error {
	select {
	case <-s.ready:
		return nil
	default:
	}
	select {
	case <-s.ready:
		return nil
	case <-s.done:
		return s.wrap("wait ready", s.doneErr)
	case <-ctx.Done():
		return s.wrap("wait ready", ctx.Err())
	}
}

func cloneMap(m map[int]host.GamepadState) map[int]host.GamepadState {
	out := make(map[int]host.GamepadState, len(m))
	for k, v := range m {
		v.Buttons = append([]float64(nil), v.Buttons...)
		v.Axes = append([]float64(nil), v.Axes...)
		out[k] = v
	}
	return out
}

func snapshot(m map[int]host.GamepadState) []host.GamepadState {
	out := make([]host.GamepadState, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// mutate edits a copy of the desired pads, sends the whole set, and waits for
// the page's receipt. If ctx ends first the change may still apply later: the
// desired state stays as sent. If the page rejects the change it reverts.
func (s *Server) mutate(ctx context.Context, op, at string, frame uint64, edit func(map[int]host.GamepadState) error) (Receipt, error) {
	s.mut.Lock()
	defer s.mut.Unlock()
	s.mu.Lock()
	conn, err := s.liveLocked()
	if err != nil {
		s.mu.Unlock()
		return Receipt{}, s.wrap(op, err)
	}
	next := cloneMap(s.desired)
	if err := edit(next); err != nil {
		s.mu.Unlock()
		return Receipt{}, fmt.Errorf("hosttest: %s: %w", op, err)
	}
	pads := snapshot(next)
	if err := host.ValidatePadStates(pads); err != nil {
		s.mu.Unlock()
		return Receipt{}, fmt.Errorf("hosttest: %s: %w", op, err)
	}
	s.nextSeq++
	seq := s.nextSeq
	s.sentSeq = seq
	w := &applyWaiter{frame: frame, ch: make(chan applyResult, 1)}
	s.applies[seq] = w
	prev := s.desired
	s.desired = next
	s.mu.Unlock()

	msg := host.PadMessage{Type: host.PadMsgSet, Seq: seq, At: at, Frame: frame, Pads: host.PadWireFrom(pads)}
	if err := s.write(conn, msg); err != nil {
		s.fail(fmt.Errorf("%w: %v", ErrPageGone, err))
		return Receipt{}, s.wrap(op, ErrPageGone)
	}
	select {
	case r := <-w.ch:
		if r.err != nil {
			var rej *RejectedError
			var late *LateFrameError
			if errors.As(r.err, &rej) || errors.As(r.err, &late) {
				s.mu.Lock()
				s.desired = prev
				s.mu.Unlock()
			}
			return Receipt{}, s.wrap(op, r.err)
		}
		return r.rcpt, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.applies, seq)
		s.mu.Unlock()
		return Receipt{}, s.wrap(op, ctx.Err())
	}
}

// SetPad replaces slot index with state. state.Index must equal index; the
// slot may be empty, which plugs it.
func (s *Server) SetPad(ctx context.Context, index int, state host.GamepadState) (Receipt, error) {
	return s.mutate(ctx, "set pad", host.PadAtNext, 0, func(m map[int]host.GamepadState) error {
		if state.Index != index {
			return fmt.Errorf("state.Index %d does not match index %d", state.Index, index)
		}
		m[index] = state
		return nil
	})
}

// Plug connects a neutral pad (17 buttons, four axes, all zero) in an empty
// slot. To replace a pad that has the same ID, Unplug first: the unplug's
// receipt guarantees the game polled the slot as absent.
func (s *Server) Plug(ctx context.Context, index int, id, mapping string) (Receipt, error) {
	return s.mutate(ctx, "plug", host.PadAtNext, 0, func(m map[int]host.GamepadState) error {
		if _, ok := m[index]; ok {
			return ErrAlreadyPlugged
		}
		m[index] = host.NeutralStandardPad(index, id, mapping)
		return nil
	})
}

// Unplug removes the pad in slot index. Other slots keep their indexes.
func (s *Server) Unplug(ctx context.Context, index int) (Receipt, error) {
	return s.mutate(ctx, "unplug", host.PadAtNext, 0, func(m map[int]host.GamepadState) error {
		if _, ok := m[index]; !ok {
			return ErrNotPlugged
		}
		delete(m, index)
		return nil
	})
}

func (s *Server) setValue(ctx context.Context, op string, index, i int, axis bool, v float64) (Receipt, error) {
	return s.mutate(ctx, op, host.PadAtNext, 0, func(m map[int]host.GamepadState) error {
		p, ok := m[index]
		if !ok {
			return ErrNotPlugged
		}
		list := p.Buttons
		if axis {
			list = p.Axes
		}
		if i < 0 || i >= len(list) {
			return fmt.Errorf("input %d outside the pad's %d", i, len(list))
		}
		list[i] = v
		return nil
	})
}

// Press sets a button to 1.
func (s *Server) Press(ctx context.Context, index, button int) (Receipt, error) {
	return s.setValue(ctx, "press", index, button, false, 1)
}

// Release sets a button to 0. A press and its release are two changes on two
// frames, so a game polling each frame sees both edges.
func (s *Server) Release(ctx context.Context, index, button int) (Receipt, error) {
	return s.setValue(ctx, "release", index, button, false, 0)
}

// Axis sets one axis to value in [-1,1].
func (s *Server) Axis(ctx context.Context, index, axis int, value float64) (Receipt, error) {
	return s.setValue(ctx, "axis", index, axis, true, value)
}

// SetAt replaces every connected pad, all at once, exactly at engine frame
// frame. It returns a *LateFrameError if that frame has begun, and
// ErrFrameNotSampled (with the receipt) if the game did not read the pads on
// that frame.
func (s *Server) SetAt(ctx context.Context, frame uint64, pads []host.GamepadState) (Receipt, error) {
	r, err := s.mutate(ctx, "set at", host.PadAtFrame, frame, func(m map[int]host.GamepadState) error {
		for k := range m {
			delete(m, k)
		}
		for _, p := range pads {
			p.Buttons = append([]float64(nil), p.Buttons...)
			p.Axes = append([]float64(nil), p.Axes...)
			m[p.Index] = p
		}
		return nil
	})
	if err == nil && r.Frame != frame {
		return r, s.wrap("set at", fmt.Errorf("%w: wanted frame %d, first read at frame %d", ErrFrameNotSampled, frame, r.Frame))
	}
	return r, err
}

// Pad returns a copy of the state the server last sent for pad index, and
// whether that pad is plugged in. A change that no receipt has confirmed yet
// counts as sent.
func (s *Server) Pad(index int) (host.GamepadState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.desired[index]
	if !ok {
		return host.GamepadState{}, false
	}
	st.Buttons = append([]float64(nil), st.Buttons...)
	st.Axes = append([]float64(nil), st.Axes...)
	return st, true
}

// Frames returns the last reported count of completed frames. It can lag; use
// Progress for a fresh count.
func (s *Server) Frames() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drawn
}

// watch waits until the page reports counts at or above the thresholds. A
// relative watch counts from the moment the page receives it. An absolute
// watch also checks the cache and wakes on any later message, so a receipt in
// flight cannot make it miss.
func (s *Server) watch(ctx context.Context, op string, drawn, sampled uint64, relative bool) (Receipt, error) {
	s.mu.Lock()
	conn, err := s.liveLocked()
	if err != nil {
		s.mu.Unlock()
		return Receipt{}, s.wrap(op, err)
	}
	if !relative && s.reachedLocked(drawn, sampled) {
		r := s.receiptLocked()
		s.mu.Unlock()
		return r, nil
	}
	s.nextWait++
	id := s.nextWait
	w := &watchWaiter{absolute: !relative, drawn: drawn, sampled: sampled, ch: make(chan applyResult, 1)}
	s.watches[id] = w
	s.mu.Unlock()
	msg := host.PadMessage{Type: host.PadMsgWatch, Watch: id, Drawn: drawn, Sampled: sampled, Relative: relative}
	if err := s.write(conn, msg); err != nil {
		s.fail(fmt.Errorf("%w: %v", ErrPageGone, err))
		return Receipt{}, s.wrap(op, ErrPageGone)
	}
	select {
	case r := <-w.ch:
		if r.err != nil {
			return Receipt{}, s.wrap(op, r.err)
		}
		return r.rcpt, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.watches, id)
		s.mu.Unlock()
		return Receipt{}, s.wrap(op, ctx.Err())
	}
}

// WaitFrames waits for n more engine frames to complete. The page takes the
// baseline when it receives the request, so the cached Frames count cannot
// skew it. It returns an error, never a silent success, if the page stalls.
func (s *Server) WaitFrames(ctx context.Context, n uint64) error {
	if n == 0 {
		return nil
	}
	_, err := s.watch(ctx, "wait frames", n, 0, true)
	return err
}

// WaitSampled waits until n more frames that read the pads have completed
// after the frame in since. The threshold is absolute (since.Sampled+n).
func (s *Server) WaitSampled(ctx context.Context, since Receipt, n uint64) error {
	_, err := s.watch(ctx, "wait sampled", 0, since.Sampled+n, false)
	return err
}

// Progress asks the page for its counters now: a fresh round trip.
func (s *Server) Progress(ctx context.Context) (Receipt, error) {
	return s.watch(ctx, "progress", 0, 0, true)
}
