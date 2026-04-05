package redis

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/odvcencio/gosx/auth"
	goredis "github.com/redis/go-redis/v9"
)

func TestMagicLinkStoreSharesTokensAcrossClients(t *testing.T) {
	mini := miniredis.RunT(t)
	clientA := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer clientA.Close()
	clientB := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer clientB.Close()

	storeA := NewMagicLinkStore(clientA, Options{Prefix: "gosx:test"})
	storeB := NewMagicLinkStore(clientB, Options{Prefix: "gosx:test"})
	expiresAt := time.Unix(1_700_000_000, 0).UTC().Add(20 * time.Minute)

	if err := storeA.Save(auth.MagicLinkToken{
		Token:     "token-123",
		Email:     "ada@example.com",
		User:      auth.User{ID: "ada", Email: "ada@example.com"},
		Next:      "/admin",
		ExpiresAt: expiresAt,
	}); err != nil {
		t.Fatalf("save token: %v", err)
	}

	record, err := storeB.Consume("token-123", expiresAt.Add(-time.Minute))
	if err != nil {
		t.Fatalf("consume token: %v", err)
	}
	if record.Email != "ada@example.com" || record.Next != "/admin" {
		t.Fatalf("unexpected consumed record %+v", record)
	}
	if _, err := storeA.Consume("token-123", expiresAt.Add(-time.Minute)); err != auth.ErrMagicLinkInvalid {
		t.Fatalf("expected invalid after consume, got %v", err)
	}
}

func TestMagicLinkStoreReturnsExpiredWithinGraceWindow(t *testing.T) {
	mini := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer client.Close()

	now := time.Unix(1_700_000_000, 0).UTC()
	store := NewMagicLinkStore(client, Options{
		Prefix:            "gosx:test",
		ExpiredTokenGrace: time.Hour,
	})
	if err := store.Save(auth.MagicLinkToken{
		Token:     "expired-token",
		Email:     "ada@example.com",
		ExpiresAt: now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("save expired token: %v", err)
	}

	if _, err := store.Consume("expired-token", now); err != auth.ErrMagicLinkExpired {
		t.Fatalf("expected expired token error, got %v", err)
	}
}
