package state

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/arrowwhi/infrastructure/connections/postgres"
)

type pick struct {
	Title string `json:"title"`
	ID    int    `json:"id"`
}

// testStore — общий контракт для всех реализаций. expire «проматывает время» так,
// чтобы состояние с ttl истекло.
func testStore(t *testing.T, store Store, ttl time.Duration, expire func()) {
	t.Helper()

	ctx := context.Background()
	key := Key{ChatID: 100, UserID: 7}

	_, err := store.Get(ctx, key)
	require.ErrorIs(t, err, ErrNotFound)

	s, err := New("awaiting_rating", pick{ID: 42, Title: "Дюна"})
	require.NoError(t, err)
	require.NoError(t, store.Set(ctx, key, s, 0))

	got, err := store.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "awaiting_rating", got.Name)

	var p pick
	require.NoError(t, got.Decode(&p))
	assert.Equal(t, pick{ID: 42, Title: "Дюна"}, p)

	// Другой участник того же чата — отдельное состояние.
	_, err = store.Get(ctx, Key{ChatID: 100, UserID: 8})
	require.ErrorIs(t, err, ErrNotFound)

	// Перезапись без данных.
	require.NoError(t, store.Set(ctx, key, State{Name: "idle"}, 0))
	got, err = store.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "idle", got.Name)
	assert.Empty(t, got.Data)

	require.NoError(t, store.Delete(ctx, key))
	_, err = store.Get(ctx, key)
	require.ErrorIs(t, err, ErrNotFound)

	// Истечение по TTL.
	require.NoError(t, store.Set(ctx, key, State{Name: "temp"}, ttl))
	_, err = store.Get(ctx, key)
	require.NoError(t, err)

	expire()

	_, err = store.Get(ctx, key)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestMemory(t *testing.T) {
	m := NewMemory()
	now := time.Now()
	m.now = func() time.Time { return now }

	testStore(t, m, time.Minute, func() { now = now.Add(2 * time.Minute) })
}

func TestRedis(t *testing.T) {
	srv := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: srv.Addr()})

	t.Cleanup(func() { _ = client.Close() })

	testStore(t, NewRedis(client, ""), time.Minute, func() { srv.FastForward(2 * time.Minute) })

	keys := srv.Keys()
	for _, k := range keys {
		assert.Contains(t, k, DefaultRedisPrefix)
	}
}

func TestPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs docker")
	}

	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("bot"),
		tcpostgres.WithUsername("bot"),
		tcpostgres.WithPassword("bot"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)

	cfg := postgres.DefaultConfig()
	cfg.Host, cfg.Port = host, int(port.Num())
	cfg.User, cfg.Password, cfg.Database = "bot", "bot", "bot"
	cfg.MinConns, cfg.MaxConns = 1, 4

	pool, err := postgres.New(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	store, err := NewPostgres(pool, "")
	require.NoError(t, err)
	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.EnsureSchema(ctx), "EnsureSchema идемпотентна")

	testStore(t, store, time.Second, func() { time.Sleep(1100 * time.Millisecond) })

	n, err := store.Cleanup(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "истёкшая запись удаляется уборкой")

	_, err = NewPostgres(pool, "bad; DROP TABLE x")
	require.Error(t, err)
}
