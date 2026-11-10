// Package identity gives players a lightweight identity without accounts. A
// person redeems a short invite code once and receives a device token, an
// HMAC-signed string the browser keeps in local storage. The token names a
// device record (with a display name) and, through a binding, a seat in a
// persistent group. There are no passwords, no email addresses, and no
// third-party login: losing the token means asking the group's organiser for a
// new invite to the same seat.
//
// State lives in a room/store, so identities survive restarts. The signing key
// comes from configuration and can rotate: tokens signed by older keys keep
// verifying while those keys stay in the ring.
package identity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"m31labs.dev/gosx/room/group"
	"m31labs.dev/gosx/room/store"
)

// Errors returned by the service.
var (
	ErrBadToken      = errors.New("identity: invalid token")
	ErrRevoked       = errors.New("identity: device revoked")
	ErrUnknownDevice = errors.New("identity: unknown device")
	// ErrBadInvite is Redeem's one answer for a code that is unknown,
	// expired or used up, so a guesser cannot tell them apart.
	ErrBadInvite = errors.New("identity: invalid invite code")
	// ErrInviteExpired and ErrInviteUsedUp tell those cases apart only for a
	// caller who passes a valid RedeemOptions.Token.
	ErrInviteExpired = errors.New("identity: invite expired")
	ErrInviteUsedUp  = errors.New("identity: invite already used")
	// ErrTooManyAttempts refuses a redemption, valid code or not, while a
	// failed-attempt budget is spent. Try again after it refills.
	ErrTooManyAttempts = errors.New("identity: too many failed invite attempts; try again later")
	ErrSeatTaken       = errors.New("identity: seat already bound to another device")
	ErrAlreadyBound    = errors.New("identity: device already has a seat in this group")
	ErrNoSuchSeat      = errors.New("identity: no such seat in group")
	ErrSeatRequired    = errors.New("identity: a seat is required")
	ErrBadSpec         = errors.New("identity: invalid invite spec")
	ErrNoKeys          = errors.New("identity: at least one signing key is required")
	ErrTokenTooOld     = errors.New("identity: token too old")
	ErrNotBound        = errors.New("identity: device has no seat in this group")
	ErrTooManyDevices  = errors.New("identity: device limit reached")
)

const (
	keyDevice  = "id/dev/"
	keyInvite  = "id/inv/"
	keySeat    = "id/seat/"  // id/seat/<group>/<seat> -> device
	keyDevSeat = "id/dseat/" // id/dseat/<device>/<group> -> seat

	// codeAlphabet leaves out 0 1 I L O and U, which look alike or spell words.
	codeAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ"
	codeLength   = 8
)

// Key is one HMAC signing secret. The first key in Config.Keys signs new
// tokens; the rest only verify.
type Key struct {
	ID     string // short label embedded in tokens: letters, digits, '-' and '_'
	Secret []byte // at least 16 bytes
}

// ParseKeys reads "id:base64secret,id2:base64secret2" (as set in an environment
// variable) into a key list, newest first.
func ParseKeys(spec string) ([]Key, error) {
	var keys []Key
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, enc, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("identity: key %q: want id:base64secret", id)
		}
		secret, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			secret, err = base64.RawURLEncoding.DecodeString(enc)
		}
		if err != nil {
			return nil, fmt.Errorf("identity: key %q: %w", id, err)
		}
		keys = append(keys, Key{ID: id, Secret: secret})
	}
	if len(keys) == 0 {
		return nil, ErrNoKeys
	}
	return keys, nil
}

// Config configures a Service.
type Config struct {
	// Keys is the signing ring, newest first. Required.
	Keys []Key
	// Now returns the current time. Default time.Now.
	Now func() time.Time
	// Groups, when set, ties seats to the registry's members: a seat must be
	// a member (unless ValidateSeat says otherwise), and removing a member or
	// deleting a group frees its seats in the same commit. The registry must
	// use the same store. This is the recommended wiring.
	Groups *group.Registry
	// ValidateSeat reports whether seat exists in group. It is checked when a
	// seat is bound and again on every Authenticate. Nil accepts any seat
	// name, or uses Groups.HasMember when Groups is set.
	ValidateSeat func(group, seat string) bool
	// RedeemBudget limits failed redemptions (unknown, expired or used-up
	// codes) across all callers. While it is spent, Redeem refuses every
	// code, valid or not, with ErrTooManyAttempts, so guessing stays slow
	// even from many addresses. Default 100 failures per minute.
	RedeemBudget Budget
	// CallerRedeemBudget limits failed redemptions per RedeemOptions.Caller,
	// so one noisy caller is refused before it spends the shared budget.
	// Default 10 failures per 10 minutes. Memory is fixed: callers share a
	// table of 4096 slots by keyed hash.
	CallerRedeemBudget Budget
	// MaxInviteUses caps Uses on one invite. Default 100.
	MaxInviteUses int
	// MaxInviteTTL caps an invite's lifetime. Default 30 days.
	MaxInviteTTL time.Duration
	// DefaultInviteTTL applies when a spec has none. Default 7 days.
	DefaultInviteTTL time.Duration
	// MaxTokenAge rejects tokens issued longer ago than this. Default 0: none.
	MaxTokenAge time.Duration
	// MaxDevices caps device records. Default 100000.
	MaxDevices int
}

// Device is a registered browser or app instance.
type Device struct {
	ID      string    `json:"id"`
	Name    string    `json:"name,omitempty"`
	Created time.Time `json:"created"`
	// Gen is the token generation; Rotate raises it and so invalidates older tokens.
	Gen     int  `json:"gen"`
	Revoked bool `json:"revoked,omitempty"`
}

// Invite is a redeemable code.
type Invite struct {
	Code    string    `json:"code"`
	Group   string    `json:"group"`
	Seat    string    `json:"seat,omitempty"` // fixed seat; empty lets the redeemer choose
	Uses    int       `json:"uses"`           // remaining
	MaxUses int       `json:"maxUses"`
	Created time.Time `json:"created"`
	Expires time.Time `json:"expires"`
}

// InviteSpec asks for a new invite.
type InviteSpec struct {
	Group string
	Seat  string        // optional
	Uses  int           // default 1
	TTL   time.Duration // default Config.DefaultInviteTTL
}

// RedeemOptions refine Redeem.
type RedeemOptions struct {
	// Name is the display name for a new device (or, if non-empty, replaces the
	// name of an existing one).
	Name string
	// Seat chooses the seat when the invite does not fix one.
	Seat string
	// Token, when set, binds the invite to the device it names instead of
	// creating a new device. A returning player uses it to join another group.
	Token string
	// Caller identifies who is redeeming, for Config.CallerRedeemBudget: the
	// client address (an IPv6 /64 rather than one address), or an account.
	// Empty applies only the shared Config.RedeemBudget.
	Caller string
}

// Session is the result of a redemption or authentication.
type Session struct {
	Device Device
	Token  string // empty from Authenticate
	Group  string
	Seat   string
}

// Service creates, signs, and checks identities. It is safe for concurrent use.
type Service struct {
	st       *store.Store
	cfg      Config
	mu       sync.Mutex
	attempts *attempts
}

// New returns a service over st.
func New(st *store.Store, cfg Config) (*Service, error) {
	if len(cfg.Keys) == 0 {
		return nil, ErrNoKeys
	}
	for _, k := range cfg.Keys {
		if !validToken(k.ID) || len(k.Secret) < 16 {
			return nil, fmt.Errorf("identity: key %q: id must be [A-Za-z0-9_-] and secret at least 16 bytes", k.ID)
		}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MaxInviteUses <= 0 {
		cfg.MaxInviteUses = 100
	}
	if cfg.MaxInviteTTL <= 0 {
		cfg.MaxInviteTTL = 30 * 24 * time.Hour
	}
	if cfg.DefaultInviteTTL <= 0 {
		cfg.DefaultInviteTTL = 7 * 24 * time.Hour
	}
	if cfg.MaxDevices <= 0 {
		cfg.MaxDevices = 100000
	}
	if cfg.Groups != nil {
		if cfg.Groups.Store() != st {
			return nil, errors.New("identity: Config.Groups must use the service's store")
		}
		if cfg.ValidateSeat == nil {
			cfg.ValidateSeat = cfg.Groups.HasMember
		}
	}
	cfg.RedeemBudget = cfg.RedeemBudget.withDefault(100, time.Minute)
	cfg.CallerRedeemBudget = cfg.CallerRedeemBudget.withDefault(10, 10*time.Minute)
	svc := &Service{st: st, cfg: cfg, attempts: newAttempts(cfg.RedeemBudget, cfg.CallerRedeemBudget, cfg.Now())}
	if cfg.Groups != nil {
		cfg.Groups.SetUnbinder(svc)
	}
	return svc, nil
}

func validToken(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ---- invites ----------------------------------------------------------

// NewInviteCode returns a random code: 8 characters from a 30-letter alphabet
// without look-alikes, written "ABCD-EFGH".
func NewInviteCode() string {
	var b [codeLength]byte
	_, _ = rand.Read(b[:])
	out := make([]byte, 0, codeLength+1)
	for i, c := range b {
		if i == codeLength/2 {
			out = append(out, '-')
		}
		out = append(out, codeAlphabet[int(c)%len(codeAlphabet)])
	}
	return string(out)
}

// NormalizeCode upper-cases a typed code and drops spaces and hyphens. It
// returns "" if the result is not a well-formed code.
func NormalizeCode(code string) string {
	var b strings.Builder
	for _, c := range strings.ToUpper(code) {
		if c == '-' || c == ' ' {
			continue
		}
		if !strings.ContainsRune(codeAlphabet, c) {
			return ""
		}
		b.WriteRune(c)
	}
	if b.Len() != codeLength {
		return ""
	}
	return b.String()
}

func displayCode(norm string) string { return norm[:codeLength/2] + "-" + norm[codeLength/2:] }

// CreateInvite makes an invite for a group.
func (s *Service) CreateInvite(spec InviteSpec) (Invite, error) {
	if !validName(spec.Group) || (spec.Seat != "" && !validName(spec.Seat)) {
		return Invite{}, ErrBadSpec
	}
	if spec.Uses == 0 {
		spec.Uses = 1
	}
	if spec.Uses < 0 || spec.Uses > s.cfg.MaxInviteUses {
		return Invite{}, fmt.Errorf("%w: uses must be 1-%d", ErrBadSpec, s.cfg.MaxInviteUses)
	}
	if spec.Seat != "" && spec.Uses != 1 {
		return Invite{}, fmt.Errorf("%w: an invite for one seat has one use", ErrBadSpec)
	}
	if spec.TTL == 0 {
		spec.TTL = s.cfg.DefaultInviteTTL
	}
	if spec.TTL < 0 || spec.TTL > s.cfg.MaxInviteTTL {
		return Invite{}, fmt.Errorf("%w: ttl must be up to %s", ErrBadSpec, s.cfg.MaxInviteTTL)
	}
	if spec.Seat != "" && s.cfg.ValidateSeat != nil && !s.cfg.ValidateSeat(spec.Group, spec.Seat) {
		return Invite{}, ErrNoSuchSeat
	}
	now := s.cfg.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	for tries := 0; tries < 10; tries++ {
		code := NewInviteCode()
		norm := NormalizeCode(code)
		if _, taken := s.st.Get(keyInvite + norm); taken {
			continue
		}
		inv := Invite{Code: code, Group: spec.Group, Seat: spec.Seat, Uses: spec.Uses, MaxUses: spec.Uses,
			Created: now, Expires: now.Add(spec.TTL)}
		if err := s.st.Put(keyInvite+norm, mustJSON(inv)); err != nil {
			return Invite{}, err
		}
		return inv, nil
	}
	return Invite{}, errors.New("identity: could not find a free invite code")
}

// Invite returns the invite for a code, expired or not. It is for organiser
// tools: unlike Redeem it tells unknown codes from expired ones and spends no
// budget, so do not expose it to players.
func (s *Service) Invite(code string) (Invite, error) {
	norm := NormalizeCode(code)
	if norm == "" {
		return Invite{}, ErrBadInvite
	}
	b, ok := s.st.Get(keyInvite + norm)
	if !ok {
		return Invite{}, ErrBadInvite
	}
	var inv Invite
	if err := json.Unmarshal(b, &inv); err != nil {
		return Invite{}, err
	}
	return inv, nil
}

// Invites lists a group's live invites, soonest to expire first.
func (s *Service) Invites(group string) []Invite {
	now := s.cfg.Now()
	var out []Invite
	s.st.Scan(keyInvite, func(_ string, v []byte) bool {
		var inv Invite
		if json.Unmarshal(v, &inv) == nil && inv.Group == group && inv.Uses > 0 && now.Before(inv.Expires) {
			out = append(out, inv)
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Expires.Before(out[j].Expires) })
	return out
}

// RevokeInvite deletes an invite.
func (s *Service) RevokeInvite(code string) error {
	norm := NormalizeCode(code)
	if norm == "" {
		return ErrBadInvite
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.st.Get(keyInvite + norm); !ok {
		return ErrBadInvite
	}
	return s.st.Delete(keyInvite + norm)
}

// PurgeExpired deletes expired and used-up invites and returns how many.
func (s *Service) PurgeExpired() (int, error) {
	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	b := &store.Batch{}
	n := 0
	s.st.Scan(keyInvite, func(k string, v []byte) bool {
		var inv Invite
		if json.Unmarshal(v, &inv) != nil || inv.Uses <= 0 || !now.Before(inv.Expires) {
			b.Delete(k)
			n++
		}
		return true
	})
	if n == 0 {
		return 0, nil
	}
	return n, s.st.Commit(b)
}

// Redeem spends one use of an invite. It creates a device (or uses the one
// named by opts.Token), binds it to a seat, and returns a signed token. The
// whole change commits as one batch.
//
// Guessing is limited inside Redeem, so a caller without its own rate limit
// is still protected. A code that is unknown, expired or used up returns
// ErrBadInvite (the same error for all three; a caller with a valid
// opts.Token gets ErrInviteExpired or ErrInviteUsedUp) and spends one attempt
// of Config.RedeemBudget and of opts.Caller's Config.CallerRedeemBudget.
// While either is spent, Redeem refuses every code with ErrTooManyAttempts
// without looking it up. Other outcomes spend nothing.
func (s *Service) Redeem(code string, opts RedeemOptions) (Session, error) {
	norm := NormalizeCode(code)
	if norm == "" {
		return Session{}, ErrBadInvite // malformed: names no code, tests nothing
	}
	now := s.cfg.Now().UTC()
	slot, ok := s.attempts.reserve(opts.Caller, now)
	if !ok {
		return Session{}, ErrTooManyAttempts
	}
	sess, missed, err := s.redeem(norm, opts, now)
	if !missed {
		s.attempts.refund(slot)
	}
	return sess, err
}

// redeem does the work of Redeem. missed reports a failed guess: no live
// invite has the code.
func (s *Service) redeem(norm string, opts RedeemOptions, now time.Time) (_ Session, missed bool, _ error) {
	detail := false
	if opts.Token != "" {
		if _, err := s.verifyLocked(opts.Token); err != nil {
			return Session{}, false, err
		}
		detail = true
	}
	// Look the code up before taking the service lock: a guess costs a store
	// read and never queues in front of real redemptions.
	if _, err := s.liveInvite(norm, now, detail); err != nil {
		return Session{}, true, err
	}
	name := SanitizeName(opts.Name)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Read it again under the lock: a concurrent redemption may have spent it.
	inv, err := s.liveInvite(norm, now, detail)
	if err != nil {
		return Session{}, false, err
	}
	seat := inv.Seat
	if seat == "" {
		seat = opts.Seat
	}
	if seat == "" {
		return Session{}, false, ErrSeatRequired
	}
	if !validName(seat) {
		return Session{}, false, ErrBadSpec
	}
	if s.cfg.ValidateSeat != nil && !s.cfg.ValidateSeat(inv.Group, seat) {
		return Session{}, false, ErrNoSuchSeat
	}
	if _, taken := s.st.Get(seatKey(inv.Group, seat)); taken {
		return Session{}, false, ErrSeatTaken
	}

	var dev Device
	b := &store.Batch{}
	if opts.Token != "" {
		// Verify again under the lock, so a concurrent Rotate or Revoke wins.
		dev, err = s.verifyLocked(opts.Token)
		if err != nil {
			return Session{}, false, err
		}
		if _, has := s.st.Get(devSeatKey(dev.ID, inv.Group)); has {
			return Session{}, false, ErrAlreadyBound
		}
		if name != "" && name != dev.Name {
			dev.Name = name
			b.Put(keyDevice+dev.ID, mustJSON(dev))
		}
	} else {
		if len(s.st.Keys(keyDevice)) >= s.cfg.MaxDevices {
			return Session{}, false, ErrTooManyDevices
		}
		dev = Device{ID: newDeviceID(), Name: name, Created: now}
		b.Put(keyDevice+dev.ID, mustJSON(dev))
	}
	inv.Uses--
	if inv.Uses == 0 {
		b.Delete(keyInvite + norm)
	} else {
		b.Put(keyInvite+norm, mustJSON(inv))
	}
	b.Put(seatKey(inv.Group, seat), []byte(dev.ID))
	b.Put(devSeatKey(dev.ID, inv.Group), []byte(seat))
	if err := s.st.Commit(b); err != nil {
		return Session{}, false, err
	}
	return Session{Device: dev, Token: s.sign(dev, now), Group: inv.Group, Seat: seat}, false, nil
}

// liveInvite returns the invite for norm if it can be redeemed at now.
// Unknown, expired and used-up codes all return ErrBadInvite unless detail
// is set.
func (s *Service) liveInvite(norm string, now time.Time, detail bool) (Invite, error) {
	inv, err := s.Invite(norm)
	if err != nil {
		return Invite{}, err
	}
	switch {
	case !now.Before(inv.Expires):
		if detail {
			return Invite{}, ErrInviteExpired
		}
		return Invite{}, ErrBadInvite
	case inv.Uses <= 0:
		if detail {
			return Invite{}, ErrInviteUsedUp
		}
		return Invite{}, ErrBadInvite
	}
	return inv, nil
}

// ---- tokens -----------------------------------------------------------

func (s *Service) sign(dev Device, issued time.Time) string {
	k := s.cfg.Keys[0]
	payload := base64.RawURLEncoding.EncodeToString([]byte(dev.ID + "|" + strconv.Itoa(dev.Gen) + "|" + strconv.FormatInt(issued.Unix(), 10)))
	return "v1." + k.ID + "." + payload + "." + mac(k, payload)
}

func mac(k Key, payload string) string {
	h := hmac.New(sha256.New, k.Secret)
	h.Write([]byte("gosx-device-v1." + k.ID + "." + payload))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

type claims struct {
	device string
	gen    int
	issued time.Time
	keyID  string
}

func (s *Service) parse(token string) (claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "v1" {
		return claims{}, ErrBadToken
	}
	var key *Key
	for i := range s.cfg.Keys {
		if s.cfg.Keys[i].ID == parts[1] {
			key = &s.cfg.Keys[i]
			break
		}
	}
	if key == nil {
		return claims{}, ErrBadToken
	}
	if !hmac.Equal([]byte(mac(*key, parts[2])), []byte(parts[3])) {
		return claims{}, ErrBadToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return claims{}, ErrBadToken
	}
	f := strings.Split(string(raw), "|")
	if len(f) != 3 {
		return claims{}, ErrBadToken
	}
	gen, err1 := strconv.Atoi(f[1])
	unix, err2 := strconv.ParseInt(f[2], 10, 64)
	if err1 != nil || err2 != nil {
		return claims{}, ErrBadToken
	}
	return claims{device: f[0], gen: gen, issued: time.Unix(unix, 0), keyID: key.ID}, nil
}

func (s *Service) verifyLocked(token string) (Device, error) {
	c, err := s.parse(token)
	if err != nil {
		return Device{}, err
	}
	if s.cfg.MaxTokenAge > 0 && s.cfg.Now().Sub(c.issued) > s.cfg.MaxTokenAge {
		return Device{}, ErrTokenTooOld
	}
	dev, err := s.device(c.device)
	if err != nil {
		return Device{}, ErrBadToken
	}
	if dev.Revoked {
		return Device{}, ErrRevoked
	}
	if dev.Gen != c.gen {
		return Device{}, ErrBadToken // rotated away
	}
	return dev, nil
}

// Verify checks a token's signature, age, generation, and revocation.
func (s *Service) Verify(token string) (Device, error) {
	return s.verifyLocked(token)
}

// Authenticate verifies a token and returns the seat it holds in group. The
// seat must still be bound to the device and still pass ValidateSeat (with
// Config.Groups: still be a member), so a removed member's device fails with
// ErrNotBound even if its binding was never freed.
func (s *Service) Authenticate(token, group string) (Session, error) {
	dev, err := s.Verify(token)
	if err != nil {
		return Session{}, err
	}
	seat, ok := s.SeatOf(dev.ID, group)
	if !ok {
		return Session{}, ErrNotBound
	}
	if holder, bound := s.DeviceAt(group, seat); !bound || holder != dev.ID {
		return Session{}, ErrNotBound
	}
	if s.cfg.ValidateSeat != nil && !s.cfg.ValidateSeat(group, seat) {
		return Session{}, ErrNotBound
	}
	return Session{Device: dev, Group: group, Seat: seat}, nil
}

// Refresh re-signs a valid token with the current signing key and returns it.
// Clients call it after a key rotation so the old key can leave the ring.
func (s *Service) Refresh(token string) (string, error) {
	dev, err := s.Verify(token)
	if err != nil {
		return "", err
	}
	return s.sign(dev, s.cfg.Now()), nil
}

// Rotate invalidates every token of the device and returns a fresh one.
func (s *Service) Rotate(deviceID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dev, err := s.device(deviceID)
	if err != nil {
		return "", err
	}
	if dev.Revoked {
		return "", ErrRevoked
	}
	dev.Gen++
	if err := s.st.Put(keyDevice+dev.ID, mustJSON(dev)); err != nil {
		return "", err
	}
	return s.sign(dev, s.cfg.Now()), nil
}

// Revoke disables a device and frees every seat bound to it, in one commit.
// The organiser can then invite someone new to those seats.
func (s *Service) Revoke(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dev, err := s.device(deviceID)
	if err != nil {
		return err
	}
	dev.Revoked = true
	b := &store.Batch{}
	b.Put(keyDevice+dev.ID, mustJSON(dev))
	prefix := keyDevSeat + dev.ID + "/"
	s.st.Scan(prefix, func(k string, v []byte) bool {
		group := strings.TrimPrefix(k, prefix)
		b.Delete(k).Delete(seatKey(group, string(v)))
		return true
	})
	return s.st.Commit(b)
}

// ---- devices and bindings ----------------------------------------------

func (s *Service) device(id string) (Device, error) {
	b, ok := s.st.Get(keyDevice + id)
	if !ok {
		return Device{}, ErrUnknownDevice
	}
	var d Device
	if err := json.Unmarshal(b, &d); err != nil {
		return Device{}, err
	}
	return d, nil
}

// Device returns a device record.
func (s *Service) Device(id string) (Device, error) { return s.device(id) }

// SetName sets a device's display name (at most 24 printable characters).
func (s *Service) SetName(deviceID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dev, err := s.device(deviceID)
	if err != nil {
		return err
	}
	dev.Name = SanitizeName(name)
	return s.st.Put(keyDevice+dev.ID, mustJSON(dev))
}

// Bind assigns a seat in group to a device.
func (s *Service) Bind(deviceID, group, seat string) error {
	if !validName(group) || !validName(seat) {
		return ErrBadSpec
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dev, err := s.device(deviceID)
	if err != nil {
		return err
	}
	if dev.Revoked {
		return ErrRevoked
	}
	if s.cfg.ValidateSeat != nil && !s.cfg.ValidateSeat(group, seat) {
		return ErrNoSuchSeat
	}
	if cur, ok := s.st.Get(seatKey(group, seat)); ok {
		if string(cur) == deviceID {
			return nil
		}
		return ErrSeatTaken
	}
	if _, ok := s.st.Get(devSeatKey(deviceID, group)); ok {
		return ErrAlreadyBound
	}
	return s.st.Commit((&store.Batch{}).Put(seatKey(group, seat), []byte(deviceID)).Put(devSeatKey(deviceID, group), []byte(seat)))
}

// Unbind frees a seat.
func (s *Service) Unbind(group, seat string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.st.Get(seatKey(group, seat))
	if !ok {
		return ErrNotBound
	}
	return s.st.Commit((&store.Batch{}).Delete(seatKey(group, seat)).Delete(devSeatKey(string(cur), group)))
}

// CommitUnbind implements group.Unbinder: it adds to b the deletes that free
// the seats members hold in group (every seat in group when members is nil)
// and commits b. It holds the service's lock through the commit, so no
// redemption or Bind can take one of those seats in between.
func (s *Service) CommitUnbind(b *store.Batch, group string, members []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if members == nil {
		prefix := keySeat + group + "/"
		s.st.Scan(prefix, func(k string, v []byte) bool {
			b.Delete(k).Delete(devSeatKey(string(v), group))
			return true
		})
	}
	for _, seat := range members {
		if dev, ok := s.st.Get(seatKey(group, seat)); ok {
			b.Delete(seatKey(group, seat)).Delete(devSeatKey(string(dev), group))
		}
	}
	return s.st.Commit(b)
}

// SeatOf returns the seat a device holds in group.
func (s *Service) SeatOf(deviceID, group string) (string, bool) {
	v, ok := s.st.Get(devSeatKey(deviceID, group))
	return string(v), ok
}

// DeviceAt returns the device bound to a seat.
func (s *Service) DeviceAt(group, seat string) (string, bool) {
	v, ok := s.st.Get(seatKey(group, seat))
	return string(v), ok
}

func seatKey(group, seat string) string      { return keySeat + group + "/" + seat }
func devSeatKey(device, group string) string { return keyDevSeat + device + "/" + group }

// validName accepts the IDs used for groups and seats: no '/' and no control
// characters, so they cannot collide with key separators.
func validName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if c == '/' || !unicode.IsPrint(c) || c == ' ' {
			return false
		}
	}
	return true
}

// SanitizeName keeps at most 24 printable characters.
func SanitizeName(name string) string {
	var b strings.Builder
	n := 0
	for _, c := range strings.TrimSpace(name) {
		if n == 24 {
			break
		}
		if unicode.IsPrint(c) {
			b.WriteRune(c)
			n++
		}
	}
	return b.String()
}

func newDeviceID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
