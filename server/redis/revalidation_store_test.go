package redis

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func TestRevalidationStoreSharesVersionsAcrossClients(t *testing.T) {
	mini := miniredis.RunT(t)
	clientA := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer clientA.Close()
	clientB := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer clientB.Close()

	storeA := NewRevalidationStore(clientA, Options{Prefix: "gosx:test"})
	storeB := NewRevalidationStore(clientB, Options{Prefix: "gosx:test"})

	pathVersion := storeA.RevalidatePath("/docs/getting-started")
	if pathVersion == 0 {
		t.Fatal("expected non-zero path version")
	}
	if got := storeB.PathVersion("/docs/getting-started/intro"); got != pathVersion {
		t.Fatalf("expected descendant path version %d, got %d", pathVersion, got)
	}
	if got := storeB.PathVersion("/docs"); got != 0 {
		t.Fatalf("expected parent path version to remain unchanged, got %d", got)
	}

	tagVersion := storeA.RevalidateTag("docs-pages")
	if tagVersion <= pathVersion {
		t.Fatalf("expected tag version to advance sequence past %d, got %d", pathVersion, tagVersion)
	}
	if got := storeB.TagVersion("docs-pages"); got != tagVersion {
		t.Fatalf("expected shared tag version %d, got %d", tagVersion, got)
	}

	rootVersion := storeB.RevalidatePath("/")
	if rootVersion <= tagVersion {
		t.Fatalf("expected root version to advance sequence past %d, got %d", tagVersion, rootVersion)
	}
	if got := storeA.PathVersion("/blog/post"); got != rootVersion {
		t.Fatalf("expected root path version %d, got %d", rootVersion, got)
	}
}

func TestReadyCheckPingsRedis(t *testing.T) {
	mini := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer client.Close()

	if err := ReadyCheck(client).CheckReady(context.Background()); err != nil {
		t.Fatalf("expected redis readiness check to pass, got %v", err)
	}

	mini.Close()
	if err := ReadyCheck(client).CheckReady(context.Background()); err == nil {
		t.Fatal("expected readiness check to fail after redis shutdown")
	}
}
