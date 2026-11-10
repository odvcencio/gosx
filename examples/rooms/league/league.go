// Package league is a small HTTP service built on room/store, room/group,
// room/identity and room/advance. It shows the pieces a long-running online
// league needs and holds no game rules: an organiser creates a group with
// seats, invites players by short code, each player's device reports "my
// match this round is finished", and the round advances when every seat has
// reported or the deadline passes, whichever comes first.
package league

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"m31labs.dev/gosx/ratelimit"
	"m31labs.dev/gosx/room/advance"
	"m31labs.dev/gosx/room/group"
	"m31labs.dev/gosx/room/identity"
	"m31labs.dev/gosx/room/store"
	"m31labs.dev/gosx/server"
)

const jobName = "round"

// Config configures a League.
type Config struct {
	Store *store.Store
	Keys  []identity.Key
	// AdminToken guards the organiser endpoints. Required.
	AdminToken string
	Now        func() time.Time
	Poll       time.Duration
	// JoinsPerMinute bounds POST /join per client address (see clientKey),
	// in front of the identity service's own failed-attempt budgets.
	// Default 10.
	JoinsPerMinute int
}

// League wires the packages together and serves the HTTP API.
type League struct {
	cfg    Config
	Groups *group.Registry
	IDs    *identity.Service
	Runner *advance.Runner
	mux    *http.ServeMux
	joins  *ratelimit.Keyed
}

// New builds a League. Call Start to run scheduled advances.
func New(cfg Config) (*League, error) {
	if cfg.AdminToken == "" {
		return nil, errors.New("league: AdminToken is required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.JoinsPerMinute <= 0 {
		cfg.JoinsPerMinute = 10
	}
	l := &League{cfg: cfg, joins: ratelimit.NewKeyed(1<<14, cfg.Now)}
	l.Groups = group.New(cfg.Store, group.Options{Now: cfg.Now})
	// Groups ties seats to members: removing a member frees its seat.
	ids, err := identity.New(cfg.Store, identity.Config{Keys: cfg.Keys, Now: cfg.Now, Groups: l.Groups})
	if err != nil {
		return nil, err
	}
	l.IDs = ids
	l.Runner = advance.New(cfg.Store, l.Groups, advance.Options{Now: cfg.Now, Poll: cfg.Poll, Logf: func(string, ...any) {}})
	if err := l.Runner.Handle("round", l.roundHandler()); err != nil {
		return nil, err
	}
	l.mux = http.NewServeMux()
	l.mux.HandleFunc("POST /groups", l.admin(l.createGroup))
	l.mux.HandleFunc("POST /groups/{id}/invites", l.admin(l.createInvite))
	l.mux.HandleFunc("POST /groups/{id}/rounds", l.admin(l.startRounds))
	l.mux.HandleFunc("GET /groups/{id}", l.getGroup)
	l.mux.HandleFunc("POST /join", l.join)
	l.mux.HandleFunc("POST /groups/{id}/finished", l.finished)
	return l, nil
}

// Start runs the scheduled advances until ctx ends.
func (l *League) Start(ctx context.Context) error { return l.Runner.Start(ctx) }

// ServeHTTP implements http.Handler.
func (l *League) ServeHTTP(w http.ResponseWriter, r *http.Request) { l.mux.ServeHTTP(w, r) }

func finishedKey(group string, round uint64, seat string) string {
	return "league/finished/" + group + "/" + strconv.FormatUint(round, 10) + "/" + seat
}

// roundHandler advances when every seat has reported, or on the deadline.
func (l *League) roundHandler() advance.Handler {
	missing := func(g string, round uint64) []string {
		var out []string
		for _, m := range l.Groups.Members(g) {
			if _, ok := l.cfg.Store.Get(finishedKey(g, round, m.ID)); !ok {
				out = append(out, m.ID)
			}
		}
		return out
	}
	return advance.Handler{
		Ready: func(_ context.Context, v advance.View) (bool, error) {
			return len(l.Groups.Members(v.Group)) > 0 && len(missing(v.Group, v.Round+1)) == 0, nil
		},
		Advance: func(_ context.Context, a advance.Advance) (advance.Result, error) {
			late := missing(a.Group, a.Round)
			state, _ := json.Marshal(map[string]any{"round": a.Round, "missing": late, "reason": a.Reason})
			return advance.Result{
				Apply: func(b *store.Batch) { b.Put(group.StateKey(a.Group), state) },
				Event: state,
			}, nil
		},
	}
}

func (l *League) admin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(tok), []byte(l.cfg.AdminToken)) != 1 {
			http.Error(w, "organiser token required", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

func (l *League) createGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID, Name string
		Seats    []string
	}
	if !decode(w, r, &req) {
		return
	}
	g, err := l.Groups.Create(req.ID, req.Name, nil)
	if err != nil {
		fail(w, err)
		return
	}
	for _, s := range req.Seats {
		if _, err := l.Groups.AddMember(g.ID, group.Member{ID: s, Name: s}); err != nil {
			fail(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, g)
}

func (l *League) createInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Seat       string
		Uses       int
		TTLSeconds int
	}
	if !decode(w, r, &req) {
		return
	}
	inv, err := l.IDs.CreateInvite(identity.InviteSpec{Group: r.PathValue("id"), Seat: req.Seat, Uses: req.Uses,
		TTL: time.Duration(req.TTLSeconds) * time.Second})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, inv)
}

func (l *League) startRounds(w http.ResponseWriter, r *http.Request) {
	var req struct{ IntervalSeconds, FirstDeadlineSeconds int }
	if !decode(w, r, &req) {
		return
	}
	j, err := l.Runner.Schedule(advance.Job{
		Group: r.PathValue("id"), Name: jobName, Kind: "round",
		Interval: time.Duration(req.IntervalSeconds) * time.Second,
		Deadline: l.cfg.Now().Add(time.Duration(req.FirstDeadlineSeconds) * time.Second),
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, j)
}

func (l *League) getGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	g, err := l.Groups.Get(id)
	if err != nil {
		fail(w, err)
		return
	}
	type seat struct {
		ID, Name string
		Claimed  bool `json:"claimed"`
	}
	var seats []seat
	for _, m := range l.Groups.Members(id) {
		s := seat{ID: m.ID}
		if dev, ok := l.IDs.DeviceAt(id, m.ID); ok {
			s.Claimed = true
			if d, err := l.IDs.Device(dev); err == nil {
				s.Name = d.Name
			}
		}
		seats = append(seats, s)
	}
	out := map[string]any{"group": g, "seats": seats, "state": json.RawMessage(orNull(l.Groups.State(id)))}
	if j, err := l.Runner.Job(id, jobName); err == nil {
		out["round"], out["deadline"] = j.Round, j.Deadline
	}
	writeJSON(w, http.StatusOK, out)
}

// join redeems an invite. Two layers limit code guessing: this per-address
// limit on every request, and identity's failed-attempt budgets inside
// Redeem (per address through Caller, and shared), which hold even without
// this one.
func (l *League) join(w http.ResponseWriter, r *http.Request) {
	key := clientKey(r)
	if !l.joins.Allow(key, l.cfg.JoinsPerMinute, time.Minute) {
		http.Error(w, "too many join attempts; try again later", http.StatusTooManyRequests)
		return
	}
	var req struct{ Code, Name, Seat, Token string }
	if !decode(w, r, &req) {
		return
	}
	s, err := l.IDs.Redeem(req.Code, identity.RedeemOptions{Name: req.Name, Seat: req.Seat, Token: req.Token, Caller: key})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": s.Token, "group": s.Group, "seat": s.Seat, "name": s.Device.Name})
}

// finished records that the caller's seat has finished its match this round.
func (l *League) finished(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s, err := l.IDs.Authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), id)
	if err != nil {
		fail(w, err)
		return
	}
	j, err := l.Runner.Job(id, jobName)
	if err != nil {
		fail(w, err)
		return
	}
	if err := l.cfg.Store.Put(finishedKey(id, j.Round+1, s.Seat), []byte("1")); err != nil {
		fail(w, err)
		return
	}
	l.Runner.Poke()
	writeJSON(w, http.StatusOK, map[string]any{"round": j.Round + 1, "seat": s.Seat})
}

// clientKey names the caller for rate limits. server.ClientIP applies the
// gosx App's trusted-proxy policy when the league is mounted in an App
// (see cmd/main.go) and is the TCP peer otherwise, so a client cannot forge
// it with a header. An IPv6 address is cut to its /64, the block one
// subscriber usually holds.
func clientKey(r *http.Request) string {
	addr := server.ClientIP(r)
	switch {
	case !addr.IsValid():
		return "invalid"
	case addr.Is6():
		return netip.PrefixFrom(addr, 64).Masked().String()
	}
	return addr.String()
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(v); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func orNull(b []byte) []byte {
	if len(b) == 0 {
		return []byte("null")
	}
	return b
}

func fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, identity.ErrTooManyAttempts):
		status = http.StatusTooManyRequests
	case errors.Is(err, group.ErrNotFound), errors.Is(err, advance.ErrNotFound), errors.Is(err, identity.ErrBadInvite):
		status = http.StatusNotFound
	case errors.Is(err, identity.ErrBadToken), errors.Is(err, identity.ErrRevoked), errors.Is(err, identity.ErrNotBound):
		status = http.StatusUnauthorized
	case errors.Is(err, group.ErrExists), errors.Is(err, identity.ErrSeatTaken), errors.Is(err, identity.ErrAlreadyBound),
		errors.Is(err, advance.ErrExists), errors.Is(err, identity.ErrInviteExpired), errors.Is(err, identity.ErrInviteUsedUp):
		status = http.StatusConflict
	case errors.Is(err, group.ErrBadID), errors.Is(err, group.ErrFull), errors.Is(err, identity.ErrBadSpec),
		errors.Is(err, identity.ErrNoSuchSeat), errors.Is(err, identity.ErrSeatRequired), errors.Is(err, advance.ErrBadJob):
		status = http.StatusBadRequest
	}
	http.Error(w, err.Error(), status)
}
