// Package redis implements cache and idempotency stores backed by Redis.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/z-alamsyah/codebase-go/internal/model"
)

// UserCache implements user.Cache. Keys follow <prefix>:user:<id>.
// model.User.PasswordHash is tagged json:"-", so it is never cached.
type UserCache struct {
	rdb    *goredis.Client
	prefix string
	ttl    time.Duration
}

func NewUserCache(rdb *goredis.Client, prefix string, ttl time.Duration) *UserCache {
	return &UserCache{rdb: rdb, prefix: prefix, ttl: ttl}
}

func (c *UserCache) key(id uuid.UUID) string {
	return fmt.Sprintf("%s:user:%s", c.prefix, id)
}

func (c *UserCache) Get(ctx context.Context, id uuid.UUID) (model.User, bool, error) {
	raw, err := c.rdb.Get(ctx, c.key(id)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return model.User{}, false, nil
	}
	if err != nil {
		return model.User{}, false, fmt.Errorf("redis get: %w", err)
	}
	var u model.User
	if err := json.Unmarshal(raw, &u); err != nil {
		return model.User{}, false, fmt.Errorf("decode cached user: %w", err)
	}
	return u, true, nil
}

func (c *UserCache) Set(ctx context.Context, u model.User) error {
	raw, err := json.Marshal(u)
	if err != nil {
		return fmt.Errorf("encode user: %w", err)
	}
	return c.rdb.Set(ctx, c.key(u.ID), raw, c.ttl).Err()
}

func (c *UserCache) Delete(ctx context.Context, id uuid.UUID) error {
	return c.rdb.Del(ctx, c.key(id)).Err()
}
