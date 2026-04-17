package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisCache 为 Profile 提供 Redis 二级缓存，降低 PG 读压力。
type redisCache struct {
	rdb *redis.Client
	ttl time.Duration
}

func (c *redisCache) key(canonicalID string) string {
	return fmt.Sprintf("cdp:profile:%s", canonicalID)
}

func (c *redisCache) set(ctx context.Context, p *Profile) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, c.key(p.CanonicalID), data, c.ttl).Err()
}

func (c *redisCache) get(ctx context.Context, canonicalID string) (*Profile, error) {
	data, err := c.rdb.Get(ctx, c.key(canonicalID)).Bytes()
	if err != nil {
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *redisCache) del(ctx context.Context, canonicalID string) error {
	return c.rdb.Del(ctx, c.key(canonicalID)).Err()
}
