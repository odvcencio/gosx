//go:build !(js && wasm)

package hubclient

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/hub"
)

type binLog struct {
	mu   sync.Mutex
	got  [][]byte
	text []string
}

func (l *binLog) addBin(b []byte) {
	l.mu.Lock()
	l.got = append(l.got, append([]byte(nil), b...))
	l.mu.Unlock()
}
func (l *binLog) bins() [][]byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([][]byte(nil), l.got...)
}

func TestClientDeliversBinaryFramesToOnBinary(t *testing.T) {
	h := hub.New("bin-in")
	server := httptest.NewServer(h)
	defer server.Close()
	var l binLog
	c := New(Options{URL: wsURL(server.URL), OnBinary: l.addBin})
	defer c.Close()
	c.On("__welcome", func(json.RawMessage) { l.mu.Lock(); l.text = append(l.text, "welcome"); l.mu.Unlock() })
	c.Connect()
	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })
	waitFor(t, time.Second, func() bool { l.mu.Lock(); defer l.mu.Unlock(); return len(l.text) == 1 })

	if n := h.BroadcastBinary([]byte{0, 1, 2, 3}); n != 1 {
		t.Fatalf("broadcast reached %d clients", n)
	}
	waitFor(t, time.Second, func() bool { return len(l.bins()) == 1 })
	if !bytes.Equal(l.bins()[0], []byte{0, 1, 2, 3}) {
		t.Fatalf("got %v", l.bins())
	}
}

func TestClientWithoutOnBinaryStillIgnoresBinary(t *testing.T) {
	h := hub.New("bin-ignore")
	server := httptest.NewServer(h)
	defer server.Close()
	c := New(Options{URL: wsURL(server.URL)})
	defer c.Close()
	got := make(chan string, 4)
	c.On("after", func(json.RawMessage) { got <- "after" })
	c.Connect()
	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })
	h.BroadcastBinary([]byte{0, 9, 9})
	h.Broadcast("after", true)
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("text event lost after an unhandled binary frame")
	}
}

func TestClientSendBinaryReachesHubHandler(t *testing.T) {
	h := hub.New("bin-out")
	recv := make(chan []byte, 4)
	h.SetBinaryMessageHandler(func(_ *hub.Client, data []byte) bool {
		recv <- append([]byte(nil), data...)
		return true
	})
	server := httptest.NewServer(h)
	defer server.Close()
	c := New(Options{URL: wsURL(server.URL)})
	defer c.Close()
	if err := c.SendBinary([]byte{0, 1}); err != ErrNotConnected {
		t.Fatalf("before connect: %v", err)
	}
	c.Connect()
	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })
	if err := c.SendBinary([]byte{0, 5, 6}); err != nil {
		t.Fatal(err)
	}
	select {
	case d := <-recv:
		if !bytes.Equal(d, []byte{0, 5, 6}) {
			t.Fatalf("got %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hub never saw the frame")
	}
	c.Close()
	if err := c.SendBinary([]byte{0, 1}); err != ErrClosed {
		t.Fatalf("after close: %v", err)
	}
}

func TestConcurrentTextAndBinarySendsDoNotRace(t *testing.T) {
	h := hub.New("bin-race")
	h.SetBinaryMessageHandler(func(*hub.Client, []byte) bool { return true })
	h.On("t", func(*hub.Context) {})
	server := httptest.NewServer(h)
	defer server.Close()
	c := New(Options{URL: wsURL(server.URL)})
	defer c.Close()
	c.Connect()
	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = c.Send("t", j)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = c.SendBinary([]byte{0, byte(j)})
			}
		}()
	}
	wg.Wait()
}
