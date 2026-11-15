package league

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/room/group"
	"m31labs.dev/gosx/room/identity"
	"m31labs.dev/gosx/room/store"
	"m31labs.dev/gosx/server"
)

var keys = []identity.Key{{ID: "k1", Secret: []byte("0123456789abcdef0123")}}

type fakeClock struct{ ns atomic.Int64 }

func (c *fakeClock) Now() time.Time          { return time.Unix(0, c.ns.Load()).UTC() }
func (c *fakeClock) Advance(d time.Duration) { c.ns.Add(int64(d)) }

type client struct {
	t   *testing.T
	srv *httptest.Server
}

func (c client) do(method, path, bearer string, body any, out any) int {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, c.srv.URL+path, &buf)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

// boot opens the store in dir and serves a League on it, as a pod start would.
func boot(t *testing.T, dir string, clk *fakeClock) (*League, client, func()) {
	t.Helper()
	l, st := newLeague(t, dir, clk, 0)
	srv := httptest.NewServer(l)
	return l, client{t, srv}, func() { srv.Close(); st.Close() }
}

func newLeague(t *testing.T, dir string, clk *fakeClock, joinsPerMinute int) (*League, *store.Store) {
	t.Helper()
	st, err := store.Open(dir, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	l, err := New(Config{Store: st, Keys: keys, AdminToken: "admin", Now: clk.Now, JoinsPerMinute: joinsPerMinute})
	if err != nil {
		t.Fatal(err)
	}
	return l, st
}

func joinStatus(t *testing.T, url, forwardedFor, code string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"code": code})
	req, _ := http.NewRequest("POST", url+"/join", bytes.NewReader(body))
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// POST /join is limited per client address, and a correct code is refused
// too once the address is over the limit. The limit resets after a minute.
func TestJoinRateLimitedPerAddress(t *testing.T) {
	clk := &fakeClock{}
	clk.ns.Store(time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC).UnixNano())
	l, st := newLeague(t, t.TempDir(), clk, 3)
	defer st.Close()
	srv := httptest.NewServer(l)
	defer srv.Close()
	if _, err := l.Groups.Create("spring", "Spring", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Groups.AddMember("spring", group.Member{ID: "red"}); err != nil {
		t.Fatal(err)
	}
	inv, err := l.IDs.CreateInvite(identity.InviteSpec{Group: "spring", Seat: "red"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if code := joinStatus(t, srv.URL, "", identity.NewInviteCode()); code != http.StatusNotFound {
			t.Fatalf("guess %d = %d, want 404", i, code)
		}
	}
	if code := joinStatus(t, srv.URL, "", inv.Code); code != http.StatusTooManyRequests {
		t.Fatalf("correct code over the limit = %d, want 429", code)
	}
	clk.Advance(time.Minute)
	if code := joinStatus(t, srv.URL, "", inv.Code); code != http.StatusOK {
		t.Fatalf("correct code after the window = %d, want 200", code)
	}
}

// Mounted in a gosx App with a trusted proxy, the limit keys by the address
// the proxy forwards, so two players behind one ingress get separate limits.
func TestJoinKeyedByTrustedProxyClient(t *testing.T) {
	clk := &fakeClock{}
	clk.ns.Store(time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC).UnixNano())
	l, st := newLeague(t, t.TempDir(), clk, 2)
	defer st.Close()
	app := server.New()
	if err := app.SetTrustedProxies(server.TrustedProxyPolicy{Proxies: []string{"127.0.0.1", "::1"}}); err != nil {
		t.Fatal(err)
	}
	app.Mount("/", l)
	srv := httptest.NewServer(app.Build())
	defer srv.Close()
	for range 2 {
		joinStatus(t, srv.URL, "203.0.113.7", identity.NewInviteCode())
	}
	if code := joinStatus(t, srv.URL, "203.0.113.7", identity.NewInviteCode()); code != http.StatusTooManyRequests {
		t.Fatalf("third join from one client = %d, want 429", code)
	}
	if code := joinStatus(t, srv.URL, "198.51.100.9", identity.NewInviteCode()); code != http.StatusNotFound {
		t.Fatalf("another client behind the same proxy = %d, want 404 (not limited)", code)
	}
}

// TestLeagueEndToEnd persists a group, invites two devices, restarts the
// server, advances a round early (everyone finished) and a second one on its
// deadline.
func TestLeagueEndToEnd(t *testing.T) {
	dir, clk := t.TempDir(), &fakeClock{}
	clk.ns.Store(time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC).UnixNano())
	l, c, stop := boot(t, dir, clk)

	if code := c.do("POST", "/groups", "", map[string]any{"id": "spring", "name": "Spring", "seats": []string{"red", "blue"}}, nil); code != 401 {
		t.Fatalf("create without organiser token = %d", code)
	}
	if code := c.do("POST", "/groups", "admin", map[string]any{"id": "spring", "name": "Spring", "seats": []string{"red", "blue"}}, nil); code != 201 {
		t.Fatalf("create group = %d", code)
	}
	var inv1, inv2 identity.Invite
	c.do("POST", "/groups/spring/invites", "admin", map[string]any{"seat": "red"}, &inv1)
	c.do("POST", "/groups/spring/invites", "admin", map[string]any{"seat": "blue"}, &inv2)
	if code := c.do("POST", "/groups/spring/rounds", "admin", map[string]any{"intervalSeconds": 86400, "firstDeadlineSeconds": 3600}, nil); code != 201 {
		t.Fatalf("schedule = %d", code)
	}
	var red, blue struct{ Token, Seat string }
	c.do("POST", "/join", "", map[string]any{"code": inv1.Code, "name": "Ada"}, &red)

	// The server restarts between the two joins.
	stop()
	l, c, stop = boot(t, dir, clk)
	defer stop()
	if code := c.do("POST", "/join", "", map[string]any{"code": inv2.Code, "name": "Bo"}, &blue); code != 200 {
		t.Fatalf("second device join after restart = %d", code)
	}
	if code := c.do("POST", "/join", "", map[string]any{"code": inv2.Code, "name": "Cy"}, nil); code == 200 {
		t.Fatal("single-use invite redeemed twice")
	}
	var view struct {
		Seats []struct {
			ID, Name string
			Claimed  bool
		}
		Round uint64
	}
	c.do("GET", "/groups/spring", "", nil, &view)
	if len(view.Seats) != 2 || view.Seats[1].Name != "Ada" || view.Seats[0].Name != "Bo" || !view.Seats[1].Claimed {
		t.Fatalf("group view %+v", view)
	}

	// Round 1: both report, so it advances before its deadline.
	ctx := context.Background()
	c.do("POST", "/groups/spring/finished", red.Token, nil, nil)
	if n, _ := l.Runner.RunDue(ctx); n != 0 {
		t.Fatal("advanced with one seat missing")
	}
	c.do("POST", "/groups/spring/finished", blue.Token, nil, nil)
	if n, _ := l.Runner.RunDue(ctx); n != 1 {
		t.Fatalf("round did not advance when all seats finished")
	}

	// Round 2: only one reports; the deadline closes it. Downtime skips nothing.
	c.do("POST", "/groups/spring/finished", red.Token, nil, nil)
	stop()
	clk.Advance(48 * time.Hour)
	l, c, stop2 := boot(t, dir, clk)
	defer stop2()
	if n, _ := l.Runner.RunDue(ctx); n != 1 {
		t.Fatalf("overdue round did not advance after restart (n=%d)", n)
	}
	var after struct {
		Round uint64
		State struct {
			Round   uint64
			Missing []string
			Reason  string
		}
	}
	c.do("GET", "/groups/spring", "", nil, &after)
	if after.Round != 2 || after.State.Reason != "deadline" || len(after.State.Missing) != 1 || after.State.Missing[0] != "blue" {
		t.Fatalf("after deadline: %+v", after)
	}
	if code := c.do("POST", "/groups/spring/finished", "garbage", nil, nil); code != 401 {
		t.Fatalf("bad token = %d", code)
	}
}
