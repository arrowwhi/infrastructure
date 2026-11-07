package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// DefaultRedisPrefix — префикс ключей состояний по умолчанию.
const DefaultRedisPrefix = "telegram:state:"

// Redis — хранилище в Redis. Истёкшие состояния удаляет сам Redis по TTL.
type Redis struct {
	client goredis.UniversalClient
	prefix string
}

// NewRedis создаёт хранилище. Подходит и *redis.Client из connections/redis
// (передавайте client.Client). prefix пустой — DefaultRedisPrefix.
func NewRedis(client goredis.UniversalClient, prefix string) *Redis {
	if prefix == "" {
		prefix = DefaultRedisPrefix
	}

	return &Redis{client: client, prefix: prefix}
}

func (r *Redis) key(k Key) string {
	return r.prefix + k.String()
}

// Get возвращает состояние или ErrNotFound.
func (r *Redis) Get(ctx context.Context, key Key) (State, error) {
	raw, err := r.client.Get(ctx, r.key(key)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return State{}, ErrNotFound
	}

	if err != nil {
		return State{}, fmt.Errorf("state: get: %w", err)
	}

	var s State
	if err = json.Unmarshal(raw, &s); err != nil {
		return State{}, fmt.Errorf("state: decode: %w", err)
	}

	return s, nil
}

// Set сохраняет состояние. ttl <= 0 — без срока жизни.
func (r *Redis) Set(ctx context.Context, key Key, s State, ttl time.Duration) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("state: encode: %w", err)
	}

	if ttl < 0 {
		ttl = 0
	}

	if err = r.client.Set(ctx, r.key(key), raw, ttl).Err(); err != nil {
		return fmt.Errorf("state: set: %w", err)
	}

	return nil
}

// Delete удаляет состояние.
func (r *Redis) Delete(ctx context.Context, key Key) error {
	if err := r.client.Del(ctx, r.key(key)).Err(); err != nil {
		return fmt.Errorf("state: delete: %w", err)
	}

	return nil
}
