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
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"m31labs.dev/gosx/examples/rooms/league"
	"m31labs.dev/gosx/room/identity"
	"m31labs.dev/gosx/room/store"
)

func main() {
	dir := flag.String("dir", envOr("LEAGUE_DIR", "league-data"), "data directory (a persistent volume in Kubernetes)")
	addr := flag.String("addr", envOr("LEAGUE_ADDR", ":8091"), "listen address")
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
	hs := &http.Server{Addr: *addr, Handler: l, ReadHeaderTimeout: 10 * time.Second}
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

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
