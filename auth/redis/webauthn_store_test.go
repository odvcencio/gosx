package redis

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"m31labs.dev/gosx/auth"
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

func TestWebAuthnStoreRejectsCredentialOverwrite(t *testing.T) {
	mini := miniredis.RunT(t)
	clientA := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer clientA.Close()
	clientB := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer clientB.Close()
	storeA := NewWebAuthnStore(clientA, Options{Prefix: "gosx:test"})
	storeB := NewWebAuthnStore(clientB, Options{Prefix: "gosx:test"})
	original := auth.WebAuthnCredential{ID: "credential", User: auth.User{ID: "ada"}, PublicKey: []byte("original key"), SignCount: 12}
	if err := storeA.SaveCredential(original); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"ada", "other"} {
		replacement := auth.WebAuthnCredential{ID: " credential ", User: auth.User{ID: owner, Roles: []string{"admin"}}, PublicKey: []byte("replacement key")}
		if err := storeB.SaveCredential(replacement); !errors.Is(err, auth.ErrWebAuthnCredentialExists) {
			t.Fatalf("duplicate error = %v", err)
		}
	}
	got, err := storeA.Credential(original.ID)
	if err != nil || !reflect.DeepEqual(got, original) {
		t.Fatalf("credential overwritten: %+v, err = %v", got, err)
	}
	owned, err := storeB.Credentials("ada")
	if err != nil || len(owned) != 1 {
		t.Fatalf("original index changed: %+v, err = %v", owned, err)
	}
	other, err := storeB.Credentials("other")
	if err != nil || len(other) != 0 {
		t.Fatalf("duplicate added another owner: %+v, err = %v", other, err)
	}
}

func TestWebAuthnStoreConcurrentInsertHasOneOwner(t *testing.T) {
	mini := miniredis.RunT(t)
	start := make(chan struct{})
	type result struct {
		userID string
		err    error
	}
	results := make(chan result, 12)
	for i := 0; i < cap(results); i++ {
		client := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
		defer client.Close()
		store := NewWebAuthnStore(client, Options{Prefix: "gosx:test"})
		userID := fmt.Sprintf("user-%d", i)
		go func() {
			<-start
			err := store.SaveCredential(auth.WebAuthnCredential{ID: "shared-credential", User: auth.User{ID: userID}, PublicKey: []byte(userID)})
			results <- result{userID: userID, err: err}
		}()
	}
	close(start)
	winner := ""
	for i := 0; i < cap(results); i++ {
		got := <-results
		if got.err == nil {
			if winner != "" {
				t.Fatal("concurrent inserts succeeded for multiple owners")
			}
			winner = got.userID
		} else if !errors.Is(got.err, auth.ErrWebAuthnCredentialExists) {
			t.Fatalf("unexpected insertion error: %v", got.err)
		}
	}
	if winner == "" {
		t.Fatal("no insertion succeeded")
	}
	client := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer client.Close()
	store := NewWebAuthnStore(client, Options{Prefix: "gosx:test"})
	credential, err := store.Credential("shared-credential")
	if err != nil || credential.User.ID != winner || string(credential.PublicKey) != winner {
		t.Fatalf("stored winner = %+v, %v", credential, err)
	}
	for i := 0; i < cap(results); i++ {
		userID := fmt.Sprintf("user-%d", i)
		owned, err := store.Credentials(userID)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if userID == winner {
			want = 1
		}
		if len(owned) != want {
			t.Fatalf("owner %s has %d credentials, want %d", userID, len(owned), want)
		}
	}
}
