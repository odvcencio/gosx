package redis

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/odvcencio/gosx/auth"
	goredis "github.com/redis/go-redis/v9"
)

func TestWebAuthnStoreSharesCredentialsAcrossClients(t *testing.T) {
	mini := miniredis.RunT(t)
	clientA := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer clientA.Close()
	clientB := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer clientB.Close()

	storeA := NewWebAuthnStore(clientA, Options{Prefix: "gosx:test"})
	storeB := NewWebAuthnStore(clientB, Options{Prefix: "gosx:test"})

	credential := auth.WebAuthnCredential{
		ID:         "cred-123",
		User:       auth.User{ID: "ada", Email: "ada@example.com"},
		PublicKey:  []byte("public-key"),
		Algorithm:  -7,
		SignCount:  2,
		Transports: []string{"internal"},
		CreatedAt:  time.Unix(1_700_000_000, 0).UTC(),
	}
	if err := storeA.SaveCredential(credential); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	got, err := storeB.Credential("cred-123")
	if err != nil {
		t.Fatalf("load credential: %v", err)
	}
	if got.User.Email != "ada@example.com" || string(got.PublicKey) != "public-key" {
		t.Fatalf("unexpected credential %+v", got)
	}

	credentials, err := storeB.Credentials("ada")
	if err != nil {
		t.Fatalf("load user credentials: %v", err)
	}
	if len(credentials) != 1 || credentials[0].ID != "cred-123" {
		t.Fatalf("unexpected user credentials %+v", credentials)
	}
}

func TestWebAuthnStoreUpdatesCountersAcrossClients(t *testing.T) {
	mini := miniredis.RunT(t)
	clientA := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer clientA.Close()
	clientB := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer clientB.Close()

	storeA := NewWebAuthnStore(clientA, Options{Prefix: "gosx:test"})
	storeB := NewWebAuthnStore(clientB, Options{Prefix: "gosx:test"})

	if err := storeA.SaveCredential(auth.WebAuthnCredential{
		ID:        "cred-123",
		User:      auth.User{ID: "ada"},
		PublicKey: []byte("public-key"),
		Algorithm: -7,
		SignCount: 1,
	}); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	usedAt := time.Unix(1_700_000_123, 0).UTC()
	if err := storeB.UpdateCounter("cred-123", 9, usedAt); err != nil {
		t.Fatalf("update counter: %v", err)
	}

	credential, err := storeA.Credential("cred-123")
	if err != nil {
		t.Fatalf("reload credential: %v", err)
	}
	if credential.SignCount != 9 || !credential.LastUsedAt.Equal(usedAt) {
		t.Fatalf("unexpected updated credential %+v", credential)
	}
}

func TestWebAuthnStoreMissingCredentialReturnsNotFound(t *testing.T) {
	mini := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer client.Close()

	store := NewWebAuthnStore(client, Options{Prefix: "gosx:test"})
	if _, err := store.Credential("missing"); err != auth.ErrWebAuthnCredentialNotFound {
		t.Fatalf("expected missing credential error, got %v", err)
	}
	if err := store.UpdateCounter("missing", 1, time.Now().UTC()); err != auth.ErrWebAuthnCredentialNotFound {
		t.Fatalf("expected missing credential error, got %v", err)
	}
}
