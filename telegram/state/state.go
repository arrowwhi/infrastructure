// Package state — прежний путь хранилища шагов диалога.
//
// Deprecated: пакет оставлен для обратной совместимости, весь код переехал в
// [github.com/arrowwhi/infrastructure/telegram]. Все типы здесь — алиасы. Новые имена:
// Key → StateKey, New → NewState, ErrNotFound → ErrStateNotFound, Memory → MemoryStore,
// Redis → RedisStore, Postgres → PostgresStore (и соответствующие конструкторы),
// PostgresSchema → PostgresStateSchema, DefaultRedisPrefix → DefaultRedisStatePrefix.
package state

import (
	goredis "github.com/redis/go-redis/v9"

	"github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/arrowwhi/infrastructure/telegram"
)

// ErrNotFound — та же переменная, что [telegram.ErrStateNotFound], errors.Is работает с обеими.
var ErrNotFound = telegram.ErrStateNotFound

// Значения по умолчанию.
const (
	DefaultPostgresTable = telegram.DefaultPostgresTable
	DefaultRedisPrefix   = telegram.DefaultRedisStatePrefix
)

type (
	Key      = telegram.StateKey
	State    = telegram.State
	Store    = telegram.Store
	Memory   = telegram.MemoryStore
	Redis    = telegram.RedisStore
	Postgres = telegram.PostgresStore
)

// New вызывает [telegram.NewState].
func New(name string, data any) (State, error) { return telegram.NewState(name, data) }

// NewMemory вызывает [telegram.NewMemoryStore].
func NewMemory() *Memory { return telegram.NewMemoryStore() }

// NewRedis вызывает [telegram.NewRedisStore].
func NewRedis(client goredis.UniversalClient, prefix string) *Redis {
	return telegram.NewRedisStore(client, prefix)
}

// NewPostgres вызывает [telegram.NewPostgresStore].
func NewPostgres(pool *postgres.Pool, table string) (*Postgres, error) {
	return telegram.NewPostgresStore(pool, table)
}

// PostgresSchema вызывает [telegram.PostgresStateSchema].
func PostgresSchema(table string) string { return telegram.PostgresStateSchema(table) }
