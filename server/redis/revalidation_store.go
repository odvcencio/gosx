package redis

import (
	"context"
	"strconv"
	"strings"

	"github.com/odvcencio/gosx/server"
	goredis "github.com/redis/go-redis/v9"
)

var revalidationBumpScript = goredis.NewScript(`
local version = redis.call("INCR", KEYS[1])
redis.call("SET", KEYS[2], version)
return version
`)

// RevalidationStore is a Redis-backed implementation of server.RevalidationStore.
type RevalidationStore struct {
	client goredis.UniversalClient
	opts   Options
}

var _ server.RevalidationStore = (*RevalidationStore)(nil)

// NewRevalidationStore creates a Redis-backed distributed revalidation store.
func NewRevalidationStore(client goredis.UniversalClient, opts Options) *RevalidationStore {
	return &RevalidationStore{
		client: client,
		opts:   opts.normalized(),
	}
}

// NewRevalidationAdapter is a compatibility alias for NewRevalidationStore.
func NewRevalidationAdapter(client goredis.UniversalClient) *RevalidationStore {
	return NewRevalidationStore(client, Options{})
}

// RevalidatePath invalidates cached responses for the provided path prefix.
func (s *RevalidationStore) RevalidatePath(target string) uint64 {
	if s == nil || s.client == nil {
		return 0
	}
	result, err := revalidationBumpScript.Run(
		context.Background(),
		s.client,
		[]string{s.opts.revalidationSeqKey(), s.opts.revalidationPathKey(target)},
	).Int64()
	if err != nil {
		return 0
	}
	return uint64(result)
}

// RevalidateTag invalidates cached responses associated with the provided tag.
func (s *RevalidationStore) RevalidateTag(tag string) uint64 {
	if s == nil || s.client == nil {
		return 0
	}
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return 0
	}
	result, err := revalidationBumpScript.Run(
		context.Background(),
		s.client,
		[]string{s.opts.revalidationSeqKey(), s.opts.revalidationTagKey(tag)},
	).Int64()
	if err != nil {
		return 0
	}
	return uint64(result)
}

// PathVersion returns the newest invalidation version that applies to requestPath.
func (s *RevalidationStore) PathVersion(requestPath string) uint64 {
	if s == nil || s.client == nil {
		return 0
	}
	candidates := pathCandidates(requestPath)
	keys := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		keys = append(keys, s.opts.revalidationPathKey(candidate))
	}
	values, err := s.client.MGet(context.Background(), keys...).Result()
	if err != nil {
		return 0
	}
	var version uint64
	for _, value := range values {
		switch current := value.(type) {
		case nil:
			continue
		case string:
			candidate, err := strconv.ParseUint(strings.TrimSpace(current), 10, 64)
			if err == nil && candidate > version {
				version = candidate
			}
		}
	}
	return version
}

// TagVersion returns the invalidation version for the provided tag.
func (s *RevalidationStore) TagVersion(tag string) uint64 {
	if s == nil || s.client == nil {
		return 0
	}
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return 0
	}
	value, err := s.client.Get(context.Background(), s.opts.revalidationTagKey(tag)).Result()
	if err != nil {
		return 0
	}
	version, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0
	}
	return version
}
