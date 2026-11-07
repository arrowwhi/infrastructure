package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// DefaultRedisStatePrefix — префикс ключей состояний по умолчанию.
const DefaultRedisStatePrefix = "telegram:state:"

// RedisStore — хранилище в Redis. Истёкшие состояния удаляет сам Redis по TTL.
type RedisStore struct {
	client goredis.UniversalClient
	prefix string
}

// NewRedisStore создаёт хранилище. Подходит и *redis.Client из connections/redis
// (передавайте client.Client). prefix пустой — DefaultRedisStatePrefix.
func NewRedisStore(client goredis.UniversalClient, prefix string) *RedisStore {
	if prefix == "" {
		prefix = DefaultRedisStatePrefix
	}

	return &RedisStore{client: client, prefix: prefix}
}

func (r *RedisStore) key(k StateKey) string {
	return r.prefix + k.String()
}

// Get возвращает состояние или ErrStateNotFound.
func (r *RedisStore) Get(ctx context.Context, key StateKey) (State, error) {
	raw, err := r.client.Get(ctx, r.key(key)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return State{}, ErrStateNotFound
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
func (r *RedisStore) Set(ctx context.Context, key StateKey, s State, ttl time.Duration) error {
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
func (r *RedisStore) Delete(ctx context.Context, key StateKey) error {
	if err := r.client.Del(ctx, r.key(key)).Err(); err != nil {
		return fmt.Errorf("state: delete: %w", err)
	}

	return nil
}
