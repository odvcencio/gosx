// Command sizeprobe is the smallest browser netplay client. The test in
// netplay/client builds it for js/wasm and checks the binary size against a
// budget; it is not meant to be run.
package main

import (
	"time"

	"m31labs.dev/gosx/netplay"
	"m31labs.dev/gosx/netplay/client"
)

func main() {
	c, err := client.Dial(client.Options{HubURL: "wss://example.invalid/hub"})
	if err != nil {
		return
	}
	var buf [netplay.MaxPacketSize]byte
	for {
		c.Recv(buf[:])
		time.Sleep(time.Millisecond)
	}
}
