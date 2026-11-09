// Command league serves the league example: persistent groups, invite codes,
// device tokens, and rounds that advance on a deadline or when every seat has
// reported.
//
//	LEAGUE_ADMIN_TOKEN=secret LEAGUE_KEYS=k1:$(openssl rand -base64 32) \
//	  go run ./examples/rooms/league/cmd -dir ./league-data -addr :8091
//
// LEAGUE_KEYS is the signing ring, newest first: "id:base64secret,id2:...".
// Add a new key at the front to rotate; keep the old one until clients have
// refreshed their tokens.
//
// Behind a proxy or ingress, set LEAGUE_TRUSTED_PROXIES (or
// -trusted-proxies) to the proxies' addresses or CIDRs, so the per-address
// limit on POST /join keys by each player's address instead of the proxy's.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"m31labs.dev/gosx/examples/rooms/league"
	"m31labs.dev/gosx/room/identity"
	"m31labs.dev/gosx/room/store"
	"m31labs.dev/gosx/server"
)

func main() {
	dir := flag.String("dir", envOr("LEAGUE_DIR", "league-data"), "data directory (a persistent volume in Kubernetes)")
	addr := flag.String("addr", envOr("LEAGUE_ADDR", ":8091"), "listen address")
	proxies := flag.String("trusted-proxies", envOr("LEAGUE_TRUSTED_PROXIES", ""), "comma-separated proxy addresses or CIDRs whose X-Forwarded-For names the client")
	flag.Parse()

	keys, err := identity.ParseKeys(os.Getenv("LEAGUE_KEYS"))
	if err != nil {
		log.Fatalf("LEAGUE_KEYS: %v", err)
	}
	// Wait up to 30 s for an old pod to release the data directory during a rollout.
	st, err := store.Open(*dir, store.Options{LockTimeout: 30 * time.Second})
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	l, err := league.New(league.Config{Store: st, Keys: keys, AdminToken: os.Getenv("LEAGUE_ADMIN_TOKEN")})
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := l.Start(ctx); err != nil {
		log.Fatal(err)
	}
	// The gosx App resolves client addresses (server.ClientIP) under the
	// trusted-proxy policy; with no proxies it uses the TCP peer.
	app := server.New()
	if err := app.SetTrustedProxies(server.TrustedProxyPolicy{Proxies: splitList(*proxies)}); err != nil {
		log.Fatalf("trusted proxies: %v", err)
	}
	app.Mount("/", l)
	hs := &http.Server{Addr: *addr, Handler: app.Build(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
		_ = l.Runner.Stop(sctx)
	}()
	s := st.Stats()
	log.Printf("league: %s, store generation %d, %d keys, recovered %d batches (%d torn bytes dropped)",
		*addr, s.Generation, s.Keys, s.RecoveredBatch, s.TruncatedBytes)
	if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
