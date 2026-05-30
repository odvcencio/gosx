// Example scheduled-work demonstrates background tasks registered on the GoSX
// scheduler with interval scheduling and watchdog (ProgressTimeout) protection.
//
// Run:  go run ./examples/scheduled-work
// Visit http://localhost:8080
//
// GET /_gosx/scheduled shows live task status for every registered task,
// including the last progress message and next-due time.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"m31labs.dev/gosx/scheduled"
	"m31labs.dev/gosx/server"
)

func main() {
	app := server.New()

	// demo.heartbeat — a simple periodic health-beat.
	// Fires every 30 seconds, calls tick.Progress to record the beat, and
	// returns nil so the scheduler marks the run successful.
	err := app.Scheduler().Register(scheduled.Task{
		Name:     "demo.heartbeat",
		Schedule: scheduled.Interval(30 * time.Second),
		Fn: func(ctx context.Context, tick scheduled.TickHandle) error {
			tick.Progress("beat")
			return nil
		},
	})
	if err != nil {
		log.Fatalf("register demo.heartbeat: %v", err)
	}

	// demo.guarded — a task protected by a ProgressTimeout watchdog.
	// The task must call tick.Progress at least once every 10 seconds or the
	// scheduler cancels the run (context.Cause(ctx) == scheduled.ErrStallTimeout)
	// and retries it — preventing the silent-hang failure mode that affected the
	// chi worker before this primitive existed.
	err = app.Scheduler().Register(scheduled.Task{
		Name:            "demo.guarded",
		Schedule:        scheduled.Interval(60 * time.Second),
		ProgressTimeout: 10 * time.Second,
		Fn: func(ctx context.Context, tick scheduled.TickHandle) error {
			// Phase 1: initial work unit.
			tick.Progress("phase 1: starting")
			select {
			case <-ctx.Done():
				// Cancelled by watchdog (stall) or hard timeout — return the cause.
				return context.Cause(ctx)
			case <-time.After(2 * time.Second):
			}

			// Phase 2: second work unit.
			// As long as tick.Progress is called within ProgressTimeout the
			// watchdog is satisfied. If this select block took >10 s without a
			// Progress call the context would be cancelled with ErrStallTimeout.
			tick.Progress("phase 2: processing")
			select {
			case <-ctx.Done():
				return context.Cause(ctx)
			case <-time.After(2 * time.Second):
			}

			tick.Progress("done")
			return nil
		},
	})
	if err != nil {
		log.Fatalf("register demo.guarded: %v", err)
	}

	fmt.Println("GoSX scheduled-work example running at http://localhost:8080")
	fmt.Println("Task status: GET http://localhost:8080/_gosx/scheduled")
	log.Fatal(app.ListenAndServe(":8080"))
}
