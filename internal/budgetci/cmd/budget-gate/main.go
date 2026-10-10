package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"m31labs.dev/gosx/internal/budgetci"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	status := budgetci.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
	cancel()
	os.Exit(status)
}
