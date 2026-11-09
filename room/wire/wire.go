// Package wire is the binary protocol between a room server (package room)
// and its clients (package game/netsync). It has no dependencies beyond the
// standard library, so it builds for js/wasm without net/http.
//
// Every frame is one binary WebSocket message. Byte 0 is 0x00, the hub's
// application prefix (the hub routes prefixes 1 to 255 to CRDT sync). Byte 1 is
// the Kind. The rest is the kind's body, built from unsigned and zigzag
// varints and length-prefixed byte strings.
//
//	client -> server: Input, Ping
//	server -> client: Welcome, Room, Pong, Keyframe, Delta
//
// Decoders never panic on hostile input: they return ErrMalformed. Decoded
// byte slices alias the frame, so a caller that keeps one past the frame's
// lifetime must copy it.
package wire

import (
	"encoding/binary"
	"errors"
	"time"
)

// Prefix is the first byte of every frame.
const Prefix byte = 0x00

// Version is the protocol version a Welcome carries. A client refuses a
// server whose version differs.
const Version = 1

// Limits that both sides enforce.
const (
	// MaxClientFrame bounds a client-to-server frame. It matches the hub's
	// inbound message limit (64 KiB).
	MaxClientFrame = 64 << 10
	// MaxStateSize bounds one decoded simulation snapshot.
	MaxStateSize = 4 << 20
	// MaxInputSize bounds one tick of one seat's input.
	MaxInputSize = 64
	// MaxRedundancy is the most ticks of input one Input frame carries.
	MaxRedundancy = 32
	// MaxSeats bounds the seats in one room.
	MaxSeats = 64
	// MaxText bounds a name, role, mode, code or token string.
	MaxText = 64
	// MaxResults bounds the results payload in a Room frame.
	MaxResults = 16 << 10
)

// Kind is byte 1 of a frame.
type Kind byte

const (
	KindInput    Kind = 'i'
	KindPing     Kind = 'p'
	KindPong     Kind = 'o'
	KindWelcome  Kind = 'w'
	KindRoom     Kind = 'r'
	KindKeyframe Kind = 'k'
	KindDelta    Kind = 'd'
)

// KindOf returns the kind of frame, or false when frame is not a wire frame.
func KindOf(frame []byte) (Kind, bool) {
	if len(frame) < 2 || frame[0] != Prefix {
		return 0, false
	}
	switch k := Kind(frame[1]); k {
	case KindInput, KindPing, KindPong, KindWelcome, KindRoom, KindKeyframe, KindDelta:
		return k, true
	}
	return 0, false
}

// Reliable reports whether a kind must arrive (lifecycle frames) rather than
// being superseded by a newer frame of the same kind.
func (k Kind) Reliable() bool { return k == KindWelcome || k == KindRoom }

// ErrMalformed reports a frame that does not decode.
var ErrMalformed = errors.New("wire: malformed frame")

// Close codes a room server sends in a WebSocket close frame. Browsers expose
// them as CloseEvent.code; netsync maps them to its fallback decisions.
const (
	// CloseRoomOver: the match ended and the room closed. Do not reconnect.
	CloseRoomOver = 4000
	// CloseReplaced: a newer connection resumed this seat. Do not reconnect.
	CloseReplaced = 4001
	// CloseSeatExpired: the reconnect grace ran out; the seat went to a bot.
	CloseSeatExpired = 4002
	// CloseNotFound: no room has the requested code.
	CloseNotFound = 4004
	// CloseRoomFull: every seat and spectator place is taken.
	CloseRoomFull = 4005
	// CloseServerFull: the server is at its room capacity.
	CloseServerFull = 4006
	// CloseBadRequest: unknown mode or invalid join parameters.
	CloseBadRequest = 4007
	// CloseIdle: the room closed because no human was connected.
	CloseIdle = 4008
	// CloseShutdown: the server is shutting down.
	CloseShutdown = 4009
	// CloseRateLimited: too many connections from this address.
	CloseRateLimited = 4029
)

// Retryable reports whether a client should try to reconnect after code.
// Codes not listed here (network failures, 1006) are retryable.
func Retryable(code int) bool {
	switch code {
	case CloseRoomOver, CloseReplaced, CloseSeatExpired, CloseNotFound, CloseRoomFull,
		CloseServerFull, CloseBadRequest, CloseIdle, CloseRateLimited:
		return false
	}
	return true
}

// ConnState is the state of a client connection, shared by netsync's Conn
// interface and room.LocalConn.
type ConnState uint8

const (
	ConnConnecting ConnState = iota
	ConnOpen
	ConnClosed
)

// Phase is a room lifecycle phase.
type Phase uint8

const (
	PhaseLobby Phase = iota + 1
	PhaseCountdown
	PhaseRunning
	PhaseResults
	PhaseClosed
)

func (p Phase) String() string {
	switch p {
	case PhaseLobby:
		return "lobby"
	case PhaseCountdown:
		return "countdown"
	case PhaseRunning:
		return "running"
	case PhaseResults:
		return "results"
	case PhaseClosed:
		return "closed"
	}
	return "unknown"
}

// SeatKind says who plays a seat.
type SeatKind uint8

const (
	SeatEmpty SeatKind = iota
	SeatHuman
	SeatBot
)

func (k SeatKind) String() string {
	switch k {
	case SeatEmpty:
		return "empty"
	case SeatHuman:
		return "human"
	case SeatBot:
		return "bot"
	}
	return "unknown"
}

// Seat flags in a snapshot's seat table.
const (
	FlagHuman     uint8 = 1 << 0 // a person holds the seat
	FlagConnected uint8 = 1 << 1 // that person is connected now
	FlagBot       uint8 = 1 << 2 // a bot produced this tick's input (fill or cover)
)

// Input carries one seat's held input for Count consecutive ticks, starting at
// First. A client resends recent ticks in every frame (redundancy), so one lost
// or late frame costs nothing.
type Input struct {
	First  uint64
	Count  int
	Inputs [MaxRedundancy][]byte
}

// Last returns the newest tick the frame carries, or First-1 when empty.
func (in *Input) Last() uint64 { return in.First + uint64(in.Count) - 1 }

// Append encodes the frame onto dst.
func (in *Input) Append(dst []byte) []byte {
	n := min(max(in.Count, 0), MaxRedundancy)
	dst = append(dst, Prefix, byte(KindInput))
	dst = binary.AppendUvarint(dst, in.First)
	dst = binary.AppendUvarint(dst, uint64(n))
	for i := 0; i < n; i++ {
		data := in.Inputs[i]
		if len(data) > MaxInputSize {
			data = data[:MaxInputSize]
		}
		dst = binary.AppendUvarint(dst, uint64(len(data)))
		dst = append(dst, data...)
	}
	return dst
}

// Decode parses an Input frame. Inputs alias frame.
func (in *Input) Decode(frame []byte) error {
	r := reader{b: frame}
	if !r.header(KindInput) {
		return ErrMalformed
	}
	in.First = r.uvarint()
	n := r.uvarint()
	if r.bad || n > MaxRedundancy || (n > 0 && in.First == 0) {
		return ErrMalformed
	}
	in.Count = int(n)
	for i := 0; i < in.Count; i++ {
		in.Inputs[i] = r.blob(MaxInputSize)
	}
	if !r.done() {
		in.Count = 0
		return ErrMalformed
	}
	return nil
}

// Ping asks the server for its clock. Sent is the client's monotonic clock in
// nanoseconds; the Pong echoes it.
type Ping struct {
	Seq  uint32
	Sent int64
}

func (p *Ping) Append(dst []byte) []byte {
	dst = append(dst, Prefix, byte(KindPing))
	dst = binary.AppendUvarint(dst, uint64(p.Seq))
	return binary.AppendVarint(dst, p.Sent)
}

func (p *Ping) Decode(frame []byte) error {
	r := reader{b: frame}
	if !r.header(KindPing) {
		return ErrMalformed
	}
	seq := r.uvarint()
	p.Sent = r.varint()
	if seq > 1<<32-1 || !r.done() {
		return ErrMalformed
	}
	p.Seq = uint32(seq)
	return nil
}

// Pong answers a Ping. Tick and Frac place the server on its tick timeline
// when it answered: Tick ticks have run and Frac/65536 of the next tick period
// has passed. Both are zero outside PhaseRunning.
type Pong struct {
	Seq  uint32
	Sent int64
	Tick uint64
	Frac uint16
}

func (p *Pong) Append(dst []byte) []byte {
	dst = append(dst, Prefix, byte(KindPong))
	dst = binary.AppendUvarint(dst, uint64(p.Seq))
	dst = binary.AppendVarint(dst, p.Sent)
	dst = binary.AppendUvarint(dst, p.Tick)
	return binary.AppendUvarint(dst, uint64(p.Frac))
}

func (p *Pong) Decode(frame []byte) error {
	r := reader{b: frame}
	if !r.header(KindPong) {
		return ErrMalformed
	}
	seq := r.uvarint()
	p.Sent = r.varint()
	p.Tick = r.uvarint()
	frac := r.uvarint()
	if seq > 1<<32-1 || frac > 0xFFFF || !r.done() {
		return ErrMalformed
	}
	p.Seq, p.Frac = uint32(seq), uint16(frac)
	return nil
}

// ServerTick returns Tick plus the fraction as one number of ticks.
func (p *Pong) ServerTick() float64 { return float64(p.Tick) + float64(p.Frac)/65536 }

// Welcome is the first wire frame a server sends on a connection. Seat is -1
// for a spectator. Token resumes the seat after a drop (empty for a
// spectator). Seed is the simulation seed of the room's match, so a client can
// build an identical simulation.
type Welcome struct {
	Version       int
	Seat          int
	Seats         int
	TickRate      int
	InputDelay    int
	SnapshotEvery int
	Seed          uint64
	Mode          string
	Code          string
	Token         string
}

func (w *Welcome) Append(dst []byte) []byte {
	dst = append(dst, Prefix, byte(KindWelcome))
	dst = binary.AppendUvarint(dst, uint64(max(w.Version, 0)))
	dst = binary.AppendVarint(dst, int64(w.Seat))
	dst = binary.AppendUvarint(dst, uint64(max(w.Seats, 0)))
	dst = binary.AppendUvarint(dst, uint64(max(w.TickRate, 0)))
	dst = binary.AppendUvarint(dst, uint64(max(w.InputDelay, 0)))
	dst = binary.AppendUvarint(dst, uint64(max(w.SnapshotEvery, 0)))
	dst = binary.AppendUvarint(dst, w.Seed)
	dst = appendText(dst, w.Mode)
	dst = appendText(dst, w.Code)
	return appendText(dst, w.Token)
}

func (w *Welcome) Decode(frame []byte) error {
	r := reader{b: frame}
	if !r.header(KindWelcome) {
		return ErrMalformed
	}
	version := r.uvarint()
	seat := r.varint()
	seats := r.uvarint()
	rate := r.uvarint()
	delay := r.uvarint()
	every := r.uvarint()
	w.Seed = r.uvarint()
	w.Mode = string(r.blob(MaxText))
	w.Code = string(r.blob(MaxText))
	w.Token = string(r.blob(MaxText))
	if !r.done() || version > 255 || seats == 0 || seats > MaxSeats || seat < -1 || seat >= int64(seats) ||
		rate == 0 || rate > 1000 || delay > 64 || every == 0 || every > 1000 {
		return ErrMalformed
	}
	w.Version, w.Seat, w.Seats = int(version), int(seat), int(seats)
	w.TickRate, w.InputDelay, w.SnapshotEvery = int(rate), int(delay), int(every)
	return nil
}

// SeatInfo describes one seat in a Room frame.
type SeatInfo struct {
	Kind      SeatKind
	Connected bool // a human seat's person is connected
	Covered   bool // a bot plays the seat while its person is away
	Role      string
	Name      string
}

// Room describes the room's lifecycle and seats. The server sends it after
// Welcome and on every change. Remaining is the time left in a timed phase
// (countdown, results, or a running match with a duration), zero otherwise.
// Results is the mode's results payload in PhaseResults.
type Room struct {
	Phase     Phase
	Remaining time.Duration
	Tick      uint64
	Seats     []SeatInfo
	Results   []byte
}

func (m *Room) Append(dst []byte) []byte {
	dst = append(dst, Prefix, byte(KindRoom), byte(m.Phase))
	ms := m.Remaining.Milliseconds()
	dst = binary.AppendUvarint(dst, uint64(max(ms, 0)))
	dst = binary.AppendUvarint(dst, m.Tick)
	n := min(len(m.Seats), MaxSeats)
	dst = binary.AppendUvarint(dst, uint64(n))
	for i := 0; i < n; i++ {
		s := m.Seats[i]
		var flags byte = byte(s.Kind) & 0x3
		if s.Connected {
			flags |= 1 << 2
		}
		if s.Covered {
			flags |= 1 << 3
		}
		dst = append(dst, flags)
		dst = appendText(dst, s.Role)
		dst = appendText(dst, s.Name)
	}
	results := m.Results
	if len(results) > MaxResults {
		results = nil
	}
	dst = binary.AppendUvarint(dst, uint64(len(results)))
	return append(dst, results...)
}

// Decode parses a Room frame. It reuses m.Seats' storage. Results aliases
// frame; Role and Name are copied.
func (m *Room) Decode(frame []byte) error {
	r := reader{b: frame}
	if !r.header(KindRoom) {
		return ErrMalformed
	}
	phase := Phase(r.byte1())
	ms := r.uvarint()
	m.Tick = r.uvarint()
	n := r.uvarint()
	if r.bad || phase < PhaseLobby || phase > PhaseClosed || n > MaxSeats || ms > uint64(24*time.Hour/time.Millisecond) {
		return ErrMalformed
	}
	m.Phase = phase
	m.Remaining = time.Duration(ms) * time.Millisecond
	m.Seats = m.Seats[:0]
	for i := uint64(0); i < n; i++ {
		flags := r.byte1()
		role := r.blob(MaxText)
		name := r.blob(MaxText)
		kind := SeatKind(flags & 0x3)
		if r.bad || kind > SeatBot || flags&^0xF != 0 {
			return ErrMalformed
		}
		m.Seats = append(m.Seats, SeatInfo{
			Kind: kind, Connected: flags&(1<<2) != 0, Covered: flags&(1<<3) != 0,
			Role: string(role), Name: string(name),
		})
	}
	m.Results = r.blob(MaxResults)
	if !r.done() {
		return ErrMalformed
	}
	return nil
}

// SeatState is one seat's entry in a snapshot's seat table. Lead is how many
// ticks of this seat's input the server held beyond the snapshot tick: the
// seat's client steers its clock so Lead stays near the room's input delay.
// Input is the input the server applied to the seat at the snapshot tick, which
// a client repeats to predict the seat.
type SeatState struct {
	Flags uint8
	Lead  uint32
	Input []byte
}

// Snapshot is a Keyframe or Delta frame. A keyframe's Body is the simulation
// snapshot after Tick. A delta's Body is a delta (see ApplyDelta) against the
// state in the keyframe of the same Epoch, never against another delta, so a
// client that misses a delta recovers on the next one.
type Snapshot struct {
	Keyframe bool
	Epoch    uint64
	Tick     uint64
	Seats    []SeatState
	Body     []byte
}

// AppendSnapshotHeader encodes a snapshot frame up to its body. Append the
// keyframe state, or a delta from DeltaEncoder.Append, after it.
func AppendSnapshotHeader(dst []byte, keyframe bool, epoch, tick uint64, seats []SeatState) []byte {
	kind := KindDelta
	if keyframe {
		kind = KindKeyframe
	}
	dst = append(dst, Prefix, byte(kind))
	dst = binary.AppendUvarint(dst, epoch)
	dst = binary.AppendUvarint(dst, tick)
	n := min(len(seats), MaxSeats)
	dst = binary.AppendUvarint(dst, uint64(n))
	for i := 0; i < n; i++ {
		s := seats[i]
		input := s.Input
		if len(input) > MaxInputSize {
			input = input[:MaxInputSize]
		}
		dst = append(dst, s.Flags)
		dst = binary.AppendUvarint(dst, uint64(s.Lead))
		dst = binary.AppendUvarint(dst, uint64(len(input)))
		dst = append(dst, input...)
	}
	return dst
}

// Decode parses a Keyframe or Delta frame. It reuses s.Seats' storage; seat
// inputs and Body alias frame.
func (s *Snapshot) Decode(frame []byte) error {
	r := reader{b: frame}
	kind, ok := KindOf(frame)
	if !ok || (kind != KindKeyframe && kind != KindDelta) {
		return ErrMalformed
	}
	r.off = 2
	s.Keyframe = kind == KindKeyframe
	s.Epoch = r.uvarint()
	s.Tick = r.uvarint()
	n := r.uvarint()
	if r.bad || s.Epoch == 0 || n > MaxSeats {
		return ErrMalformed
	}
	s.Seats = s.Seats[:0]
	for i := uint64(0); i < n; i++ {
		flags := r.byte1()
		lead := r.uvarint()
		input := r.blob(MaxInputSize)
		if r.bad || lead > 1<<32-1 {
			return ErrMalformed
		}
		s.Seats = append(s.Seats, SeatState{Flags: flags, Lead: uint32(lead), Input: input})
	}
	if r.bad {
		return ErrMalformed
	}
	s.Body = r.b[r.off:]
	if s.Keyframe && len(s.Body) > MaxStateSize {
		return ErrMalformed
	}
	return nil
}

func appendText(dst []byte, s string) []byte {
	if len(s) > MaxText {
		s = s[:MaxText]
	}
	dst = binary.AppendUvarint(dst, uint64(len(s)))
	return append(dst, s...)
}

// reader decodes varints with a sticky failure flag, so a decoder checks once.
type reader struct {
	b   []byte
	off int
	bad bool
}

func (r *reader) header(k Kind) bool {
	if len(r.b) < 2 || r.b[0] != Prefix || Kind(r.b[1]) != k {
		r.bad = true
		return false
	}
	r.off = 2
	return true
}

func (r *reader) uvarint() uint64 {
	if r.bad {
		return 0
	}
	v, n := binary.Uvarint(r.b[r.off:])
	if n <= 0 {
		r.bad = true
		return 0
	}
	r.off += n
	return v
}

func (r *reader) varint() int64 {
	if r.bad {
		return 0
	}
	v, n := binary.Varint(r.b[r.off:])
	if n <= 0 {
		r.bad = true
		return 0
	}
	r.off += n
	return v
}

func (r *reader) byte1() byte {
	if r.bad || r.off >= len(r.b) {
		r.bad = true
		return 0
	}
	b := r.b[r.off]
	r.off++
	return b
}

func (r *reader) blob(limit int) []byte {
	n := r.uvarint()
	if r.bad || n > uint64(limit) || n > uint64(len(r.b)-r.off) {
		r.bad = true
		return nil
	}
	b := r.b[r.off : r.off+int(n) : r.off+int(n)]
	r.off += int(n)
	return b
}

func (r *reader) done() bool { return !r.bad && r.off == len(r.b) }
