package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/odvcencio/gosx/auth"
	goredis "github.com/redis/go-redis/v9"
)

var consumeMagicLinkScript = goredis.NewScript(`
local value = redis.call("GET", KEYS[1])
if not value then
	return {0}
end
redis.call("DEL", KEYS[1])
return {1, value}
`)

// MagicLinkStore is a Redis-backed implementation of auth.MagicLinkStore.
type MagicLinkStore struct {
	client goredis.UniversalClient
	opts   Options
}

var _ auth.MagicLinkStore = (*MagicLinkStore)(nil)

// NewMagicLinkStore creates a Redis-backed durable magic-link token store.
func NewMagicLinkStore(client goredis.UniversalClient, opts Options) *MagicLinkStore {
	return &MagicLinkStore{
		client: client,
		opts:   opts.normalized(),
	}
}

// NewMagicLinkAdapter is a compatibility alias for NewMagicLinkStore.
func NewMagicLinkAdapter(client goredis.UniversalClient) *MagicLinkStore {
	return NewMagicLinkStore(client, Options{})
}

// Save stores or replaces a magic-link token.
func (s *MagicLinkStore) Save(token auth.MagicLinkToken) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("magic link store is nil")
	}
	payload, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return s.client.Set(
		context.Background(),
		s.opts.magicLinkTokenKey(token.Token),
		payload,
		s.ttlForToken(token.ExpiresAt),
	).Err()
}

// Consume validates and removes a token from the store.
func (s *MagicLinkStore) Consume(token string, now time.Time) (auth.MagicLinkToken, error) {
	if s == nil || s.client == nil {
		return auth.MagicLinkToken{}, auth.ErrMagicLinkInvalid
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return auth.MagicLinkToken{}, auth.ErrMagicLinkInvalid
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result, err := consumeMagicLinkScript.Run(
		context.Background(),
		s.client,
		[]string{s.opts.magicLinkTokenKey(token)},
	).Result()
	if err != nil {
		return auth.MagicLinkToken{}, err
	}
	reply, ok := result.([]interface{})
	if !ok || len(reply) == 0 || intReply(reply[0]) == 0 {
		return auth.MagicLinkToken{}, auth.ErrMagicLinkInvalid
	}
	payload, ok := reply[1].(string)
	if !ok || payload == "" {
		return auth.MagicLinkToken{}, auth.ErrMagicLinkInvalid
	}
	var record auth.MagicLinkToken
	if err := json.Unmarshal([]byte(payload), &record); err != nil {
		return auth.MagicLinkToken{}, err
	}
	if !record.ExpiresAt.IsZero() && now.After(record.ExpiresAt) {
		return auth.MagicLinkToken{}, auth.ErrMagicLinkExpired
	}
	return record, nil
}

func (s *MagicLinkStore) ttlForToken(expiresAt time.Time) time.Duration {
	if expiresAt.IsZero() {
		return 0
	}
	ttl := time.Until(expiresAt.UTC()) + s.opts.ExpiredTokenGrace
	if ttl > 0 {
		return ttl
	}
	if s.opts.ExpiredTokenGrace > 0 {
		return s.opts.ExpiredTokenGrace
	}
	return time.Second
}

func intReply(value interface{}) int64 {
	switch current := value.(type) {
	case int64:
		return current
	case uint64:
		return int64(current)
	default:
		return 0
	}
}
