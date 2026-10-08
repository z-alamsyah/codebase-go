package redis

import (
	"context"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// IdempotencyStore implements user.IdempotencyStore with SET NX.
type IdempotencyStore struct {
	rdb    *goredis.Client
	prefix string
}

func NewIdempotencyStore(rdb *goredis.Client, prefix string) *IdempotencyStore {
	return &IdempotencyStore{rdb: rdb, prefix: prefix}
}

func (s *IdempotencyStore) key(k string) string { return s.prefix + ":idempotency:" + k }

func (s *IdempotencyStore) Acquire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	res, err := s.rdb.SetArgs(ctx, s.key(key), 1, goredis.SetArgs{Mode: "NX", TTL: ttl}).Result()
	if errors.Is(err, goredis.Nil) {
		return false, nil // key already exists
	}
	if err != nil {
		return false, err
	}
	return res == "OK", nil
}

func (s *IdempotencyStore) Release(ctx context.Context, key string) error {
	return s.rdb.Del(ctx, s.key(key)).Err()
}
