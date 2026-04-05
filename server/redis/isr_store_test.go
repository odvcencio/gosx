package redis

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/odvcencio/gosx/server"
	goredis "github.com/redis/go-redis/v9"
)

func TestISRStorePersistsArtifactsAndStateAcrossClients(t *testing.T) {
	mini := miniredis.RunT(t)
	clientA := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer clientA.Close()
	clientB := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer clientB.Close()

	storeA := NewISRStore(clientA, Options{Prefix: "gosx:test"})
	storeB := NewISRStore(clientB, Options{Prefix: "gosx:test"})

	info, err := storeA.WriteArtifact("/app/dist/static", "/docs", "docs/index.html", []byte("<html>cached</html>"))
	if err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if info.ModTime.IsZero() {
		t.Fatal("expected artifact mod time")
	}

	stat, err := storeB.StatArtifact("/app/dist/static", "/docs", "docs/index.html")
	if err != nil {
		t.Fatalf("stat artifact: %v", err)
	}
	if !stat.ModTime.Equal(info.ModTime) {
		t.Fatalf("expected shared mod time %v, got %v", info.ModTime, stat.ModTime)
	}

	artifact, err := storeB.ReadArtifact("/app/dist/static", "/docs", "docs/index.html")
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if got := string(artifact.Body); got != "<html>cached</html>" {
		t.Fatalf("unexpected artifact body %q", got)
	}

	fallback := time.Unix(1700000000, 0).UTC()
	state, err := storeA.LoadState("/app/dist", "/docs", fallback)
	if err != nil {
		t.Fatalf("load initial state: %v", err)
	}
	if !state.GeneratedAt.Equal(fallback) {
		t.Fatalf("expected fallback generatedAt %v, got %v", fallback, state.GeneratedAt)
	}

	state.PathVersion = 42
	state.TagVersions["docs-pages"] = 7
	if err := storeA.SaveState("/app/dist", "/docs", state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	shared, err := storeB.LoadState("/app/dist", "/docs", time.Time{})
	if err != nil {
		t.Fatalf("load shared state: %v", err)
	}
	if shared.PathVersion != 42 || shared.TagVersions["docs-pages"] != 7 {
		t.Fatalf("unexpected shared state %+v", shared)
	}
}

func TestISRStoreRefreshLeaseIsDistributed(t *testing.T) {
	mini := miniredis.RunT(t)
	clientA := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer clientA.Close()
	clientB := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer clientB.Close()

	storeA := NewISRStore(clientA, Options{Prefix: "gosx:test"})
	storeB := NewISRStore(clientB, Options{Prefix: "gosx:test"})

	leaseA, acquired, err := storeA.AcquireRefresh("/app/dist", "/docs")
	if err != nil {
		t.Fatalf("acquire first lease: %v", err)
	}
	if !acquired || leaseA == nil {
		t.Fatal("expected first lease acquisition to succeed")
	}

	leaseB, acquired, err := storeB.AcquireRefresh("/app/dist", "/docs")
	if err != nil {
		t.Fatalf("acquire competing lease: %v", err)
	}
	if acquired || leaseB != nil {
		t.Fatal("expected competing lease acquisition to fail")
	}

	if err := leaseA.Release(); err != nil {
		t.Fatalf("release first lease: %v", err)
	}

	leaseB, acquired, err = storeB.AcquireRefresh("/app/dist", "/docs")
	if err != nil {
		t.Fatalf("acquire second lease: %v", err)
	}
	if !acquired || leaseB == nil {
		t.Fatal("expected lease acquisition after release to succeed")
	}
	if err := leaseB.Release(); err != nil {
		t.Fatalf("release second lease: %v", err)
	}
}

func TestISRStoreMissingArtifactReturnsNotFound(t *testing.T) {
	mini := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer client.Close()

	store := NewISRStore(client, Options{Prefix: "gosx:test"})
	if _, err := store.ReadArtifact("/app/dist/static", "/docs", "docs/index.html"); err != server.ErrISRArtifactNotFound {
		t.Fatalf("expected not found error, got %v", err)
	}
}
