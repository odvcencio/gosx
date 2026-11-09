//go:build js && wasm

// wasmclock reports Go's view of time to the page through window.goLog, so the
// drive spike can check whether time.Now, time.Sleep and time.Ticker follow
// the probe's virtual clock.
package main

import (
	"syscall/js"
	"time"
)

func main() {
	logf := js.Global().Get("goLog")
	log := func(name string, v float64) { logf.Invoke(name, v) }
	t0 := time.Now()
	log("start.unixms", float64(t0.UnixNano())/1e6)
	time.Sleep(200 * time.Millisecond)
	log("sleep.elapsed", float64(time.Since(t0).Nanoseconds())/1e6)
	log("sleep.unixms", float64(time.Now().UnixNano())/1e6)
	tk := time.NewTicker(100 * time.Millisecond)
	for i := 0; i < 3; i++ {
		<-tk.C
		log("tick.elapsed", float64(time.Since(t0).Nanoseconds())/1e6)
	}
	tk.Stop()
	log("done", float64(time.Since(t0).Nanoseconds())/1e6)
}
