package hub

import (
	"context"
	"log"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestPeerCloseTextNeverEntersReadDiagnostics(t *testing.T) {
	for _, code := range []int{websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.ClosePolicyViolation} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			h := New("synthetic-room")
			o := newObservedHub()
			if _, err := h.UseObserver(o); err != nil {
				t.Fatal(err)
			}
			buf := &syncLogBuffer{}
			previous := log.Writer()
			log.SetOutput(buf)
			defer log.SetOutput(previous)
			c := observedConnection(t, h)
			if _, _, err := c.ReadMessage(); err != nil {
				t.Fatal(err)
			}
			const canary = "peer-close-secret-canary"
			if err := c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, canary), time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			receiveHubEvent(t, o.disconnects)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := h.Close(ctx); err != nil {
				t.Fatal(err)
			}
			output := buf.String()
			if strings.Contains(output, canary) {
				t.Fatal("peer close text leaked into a read diagnostic")
			}
			logged := strings.Contains(output, "[gosx hub] read failed")
			if code == websocket.ClosePolicyViolation && !logged {
				t.Fatal("unexpected closure lost its fixed diagnostic")
			}
			if code != websocket.ClosePolicyViolation && logged {
				t.Fatal("normal closure logged as a read failure")
			}
		})
	}
}
