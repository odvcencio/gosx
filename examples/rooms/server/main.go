// Command server runs a room server for the roomtest bodies game. Pair it
// with the static client in ../static, served from any origin listed in
// -origins.
//
//	go run ./examples/rooms/server -addr :8090 -origins http://localhost:8000
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"m31labs.dev/gosx/room"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/roomtest"
)

func main() {
	addr := flag.String("addr", ":8090", "listen address")
	origins := flag.String("origins", "http://localhost:8000", "comma-separated browser origins allowed to connect")
	static := flag.String("static", "", "optional directory to serve at / (same-origin hosting)")
	flag.Parse()

	srv, err := room.NewServer(room.Config{
		Modes: []mode.Mode{
			roomtest.Mode(roomtest.Options{Name: "duel", Seats: 2, Props: 6, Duration: 2 * time.Minute}),
			roomtest.Mode(roomtest.Options{Name: "party", Seats: 6, Props: 12, Duration: 3 * time.Minute, LobbyWait: 5 * time.Second}),
		},
		AllowedOrigins: splitList(*origins),
	})
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/play/", http.StripPrefix("/play", srv))
	if *static != "" {
		mux.Handle("/", http.FileServer(http.Dir(*static)))
	}
	hs := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Close(shutdown)
		_ = hs.Shutdown(shutdown)
	}()
	log.Printf("rooms: ws://localhost%s/play/ws  lobby: http://localhost%s/play/rooms", *addr, *addr)
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
