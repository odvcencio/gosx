package checkers

import "testing"

func TestPublicHubHasBoundedBrowserPolicy(t *testing.T) {
	if Hub.MaxClients != 24 || !Hub.RequireOrigin {
		t.Fatalf("connection policy = clients %d, require origin %v", Hub.MaxClients, Hub.RequireOrigin)
	}
	if Hub.MaxMessagesPerSecond != 20 || Hub.MaxMessageBurst != 40 {
		t.Fatalf("message policy = rate %d, burst %d", Hub.MaxMessagesPerSecond, Hub.MaxMessageBurst)
	}
}
