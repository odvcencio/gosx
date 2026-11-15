# scheduled-work

Background tasks on the GoSX scheduler: an interval task with a watchdog
(`ProgressTimeout`) that records progress each run. Task status is private by
default; `App.ScheduledStatusHandler` can be mounted behind your own auth, and
it omits progress and error text.

## Run it

```sh
go run ./examples/scheduled-work
```

The server listens on <http://localhost:8080> and serves no pages. Watch the
tasks run in the process log; each run records its progress.

## What to read

- `main.go` registers tasks with `app.Scheduler().Register(scheduled.Task{...})`.
- Package `scheduled` documents the schedule types and the watchdog.
