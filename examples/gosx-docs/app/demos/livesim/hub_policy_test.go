package livesim

import "testing"

func TestPublicHubHasBoundedBrowserPolicy(t *testing.T) {
	if Hub.MaxClients != 64 || !Hub.RequireOrigin {
		t.Fatalf("connection policy = clients %d, require origin %v", Hub.MaxClients, Hub.RequireOrigin)
	}
	if Hub.MaxMessagesPerSecond != 60 || Hub.MaxMessageBurst != 120 {
		t.Fatalf("message policy = rate %d, burst %d", Hub.MaxMessagesPerSecond, Hub.MaxMessageBurst)
	}
}
