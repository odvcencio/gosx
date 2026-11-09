// Command server runs a room server for the roomtest bodies game. Pair it
// with the static client in ../static, served from any origin listed in
// -origins, or pass -static to serve the client from the server itself.
//
//	go run ./examples/rooms/server -addr :8090 -origins http://localhost:8000
//
// Every flag also reads an environment variable (ROOMS_ADDR, ROOMS_ORIGINS,
// ...), which is how the container in ../deploy is configured. GET /healthz
// reports that the process is up; GET /readyz that it accepts players; GET
// /whoami echoes the caller's peer address and X-Forwarded-For chain.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"m31labs.dev/gosx/room"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/roomtest"
)

func main() {
	addr := flag.String("addr", env("ROOMS_ADDR", ":8090"), "listen address")
	origins := flag.String("origins", env("ROOMS_ORIGINS", "http://localhost:8000"), "comma-separated browser origins allowed to connect, besides the server's own")
	static := flag.String("static", env("ROOMS_STATIC", ""), "optional directory to serve at / (same-origin hosting)")
	requireOrigin := flag.Bool("require-origin", envBool("ROOMS_REQUIRE_ORIGIN", false), "refuse connections without an Origin header (non-browser clients)")
	trustProxy := flag.Bool("trust-forwarded-for", envBool("ROOMS_TRUST_FORWARDED_FOR", false), "key rate limits by the X-Forwarded-For address the proxy appends")
	maxRooms := flag.Int("max-rooms", envInt("ROOMS_MAX_ROOMS", 0), "live room cap (0: room default, 256)")
	connects := flag.Int("connects-per-minute", envInt("ROOMS_CONNECTS_PER_MINUTE", 0), "WebSocket connections per client address per minute (0: default, 30)")
	creates := flag.Int("creates-per-minute", envInt("ROOMS_CREATES_PER_MINUTE", 0), "new rooms per client address per minute (0: default, 10)")
	flag.Parse()

	srv, err := room.NewServer(room.Config{
		Modes: []mode.Mode{
			roomtest.Mode(roomtest.Options{Name: "duel", Seats: 2, Props: 6, Duration: 2 * time.Minute}),
			roomtest.Mode(roomtest.Options{Name: "party", Seats: 6, Props: 12, Duration: 3 * time.Minute, LobbyWait: 5 * time.Second}),
		},
		AllowedOrigins:    splitList(*origins),
		RequireOrigin:     *requireOrigin,
		TrustForwardedFor: *trustProxy,
		MaxRooms:          *maxRooms,
		ConnectsPerMinute: *connects,
		CreatesPerMinute:  *creates,
	})
	if err != nil {
		log.Fatal(err)
	}
	var draining atomic.Bool
	mux := http.NewServeMux()
	mux.Handle("/play/", http.StripPrefix("/play", srv))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if draining.Load() {
			http.Error(w, "draining", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ready\n"))
	})
	// GET /whoami echoes the caller's own peer address and X-Forwarded-For
	// chain, to check what a proxy passes on (rate limits key on it).
	mux.HandleFunc("GET /whoami", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		_, _ = fmt.Fprintf(w, "remote=%s forwarded=%q\n", r.RemoteAddr, r.Header.Values("X-Forwarded-For"))
	})
	if *static != "" {
		mux.Handle("/", http.FileServer(http.Dir(*static)))
	}
	hs := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		draining.Store(true)
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Close(shutdown) // clients get close code 4009 and reconnect
		_ = hs.Shutdown(shutdown)
	}()
	log.Printf("rooms: listening on %s (WebSocket /play/ws, lobby /play/rooms, origins %v)", *addr, splitList(*origins))
	if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
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
