package hub

import (
	"errors"
	"net"

	"github.com/gorilla/websocket"
)

func (c *Client) setDisconnectReason(reason, appReason string) {
	c.mu.Lock()
	if c.disconnect.Reason == "" {
		c.disconnect = DisconnectEvent{Reason: reason, AppReason: appReason}
	}
	c.mu.Unlock()
}

func readDisconnectReason(err error) string {
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
		return "peer_closed"
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return "read_timeout"
	}
	return "read_error"
}
