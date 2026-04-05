package redis

import (
	"context"
	"errors"

	"github.com/odvcencio/gosx/server"
	goredis "github.com/redis/go-redis/v9"
)

// ReadyCheck returns a GoSX readiness check backed by Redis PING.
func ReadyCheck(client goredis.UniversalClient) server.ReadyCheck {
	return server.ReadyCheckFunc(func(ctx context.Context) error {
		if client == nil {
			return errors.New("redis client is nil")
		}
		if ctx == nil {
			ctx = context.Background()
		}
		return client.Ping(ctx).Err()
	})
}
