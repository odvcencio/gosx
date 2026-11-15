package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

type boothServer struct {
	ln      net.Listener
	handler http.Handler
}

func (b *boothServer) serve(ctx context.Context) error {
	srv := &http.Server{Handler: b.handler, ReadHeaderTimeout: 10 * time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(b.ln) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := srv.Shutdown(shutdown)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		return err
	}
}
