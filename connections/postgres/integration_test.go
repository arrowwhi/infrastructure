package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tc_postgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Пароль намеренно содержит спецсимволы: так проверяется, что сборка DSN
// действительно их экранирует, а не только выглядит правильно в юнит-тесте.
const (
	testUser     = "svc"
	testPassword = "p@ss w0rd:/?#&="
	testDatabase = "orders"
)

type testUserRow struct {
	Name  string `db:"name"`
	Email string `db:"email"`
	ID    int64  `db:"id"`
}

// startPostgres поднимает контейнер PostgreSQL на время тестов пакета.
func startPostgres(t *testing.T) Config {
	t.Helper()

	ctx := context.Background()

	container, err := tc_postgres.Run(ctx, "postgres:17-alpine",
		tc_postgres.WithDatabase(testDatabase),
		tc_postgres.WithUsername(testUser),
		tc_postgres.WithPassword(testPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, testcontainers.TerminateContainer(container))
	})

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)

	cfg := DefaultConfig()
	cfg.Host = host
	cfg.Port = int(port.Num())
	cfg.User = testUser
	cfg.Password = testPassword
	cfg.Database = testDatabase
	cfg.MinConns = 1
	cfg.MaxConns = 10

	return cfg
}

// newTestPool открывает пул и создаёт чистую таблицу users.
func newTestPool(t *testing.T, cfg Config, opts ...Option) *Pool {
	t.Helper()

	pool, err := New(t.Context(), cfg, opts...)
	require.NoError(t, err)

	t.Cleanup(pool.Close)

	_, err = pool.Exec(t.Context(), `
		DROP TABLE IF EXISTS users;
		CREATE TABLE users (
			id    bigserial PRIMARY KEY,
			name  text NOT NULL,
			email text NOT NULL UNIQUE
		)`)
	require.NoError(t, err)

	return pool
}

func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration tests need docker")
	}

	cfg := startPostgres(t)

	t.Run("connect with special characters in password", func(t *testing.T) {
		pool := newTestPool(t, cfg)
		require.NoError(t, pool.Healthcheck(t.Context()))
	})

	t.Run("instrumentation is off by default", func(t *testing.T) {
		pool := newTestPool(t, cfg)

		// Ключевое свойство выключенной инструментации: трейсер вообще не создан,
		// а значит pgx не тратит на него ни такта.
		assert.Nil(t, pool.Raw().Config().ConnConfig.Tracer)
	})

	t.Run("instrumentation is wired when enabled", func(t *testing.T) {
		pool := newTestPool(t, cfg, WithTracing(), WithMetrics(), WithSlowQueryLog(time.Nanosecond))

		tracer := pool.Raw().Config().ConnConfig.Tracer
		require.NotNil(t, tracer)
		assert.IsType(t, &compositeTracer{}, tracer)

		// Инструментированный пул обязан продолжать работать как обычный.
		ctx := WithQueryName(t.Context(), "user.count")
		count, err := Get[int64](ctx, pool, `SELECT count(*) FROM users`)
		require.NoError(t, err)
		assert.Zero(t, count)
	})

	t.Run("get and select", func(t *testing.T) {
		pool := newTestPool(t, cfg)
		ctx := t.Context()

		id, err := Get[int64](ctx, pool,
			`INSERT INTO users (name, email) VALUES ($1, $2) RETURNING id`, "Ada", "ada@example.com")
		require.NoError(t, err)
		assert.Positive(t, id)

		user, err := Get[testUserRow](ctx, pool, `SELECT id, name, email FROM users WHERE id = $1`, id)
		require.NoError(t, err)
		assert.Equal(t, "Ada", user.Name)
		assert.Equal(t, "ada@example.com", user.Email)

		users, err := Select[testUserRow](ctx, pool, `SELECT id, name, email FROM users ORDER BY id`)
		require.NoError(t, err)
		require.Len(t, users, 1)

		_, err = Get[testUserRow](ctx, pool, `SELECT id, name, email FROM users WHERE id = -1`)
		require.Error(t, err)
		assert.True(t, IsNoRows(err), "отсутствие строк должно распознаваться через IsNoRows")

		empty, err := Select[testUserRow](ctx, pool, `SELECT id, name, email FROM users WHERE id = -1`)
		require.NoError(t, err, "пустая выборка — не ошибка")
		assert.Empty(t, empty)
	})

	t.Run("unique violation is recognised", func(t *testing.T) {
		pool := newTestPool(t, cfg)
		ctx := t.Context()

		_, err := pool.Exec(ctx, `INSERT INTO users (name, email) VALUES ($1, $2)`, "Ada", "dup@example.com")
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `INSERT INTO users (name, email) VALUES ($1, $2)`, "Bob", "dup@example.com")
		require.Error(t, err)
		assert.True(t, IsUniqueViolation(err))
		assert.Equal(t, SQLStateUniqueViolation, SQLState(err))
	})

	t.Run("transaction commits and rolls back", func(t *testing.T) {
		pool := newTestPool(t, cfg)
		ctx := t.Context()

		require.NoError(t, pool.InTx(ctx, func(ctx context.Context) error {
			_, err := pool.Exec(ctx, `INSERT INTO users (name, email) VALUES ($1, $2)`, "Ada", "commit@example.com")

			return err
		}))

		sentinel := errors.New("boom")
		err := pool.InTx(ctx, func(ctx context.Context) error {
			if _, execErr := pool.Exec(ctx, `INSERT INTO users (name, email) VALUES ($1, $2)`, "Bob", "rollback@example.com"); execErr != nil {
				return execErr
			}

			return sentinel
		})
		require.ErrorIs(t, err, sentinel)

		emails, err := Select[string](ctx, pool, `SELECT email FROM users ORDER BY email`)
		require.NoError(t, err)
		assert.Equal(t, []string{"commit@example.com"}, emails, "откат должен убрать вставленную строку")
	})

	t.Run("repository code is unaware of the transaction", func(t *testing.T) {
		pool := newTestPool(t, cfg)
		ctx := t.Context()

		// Один и тот же вызов работает и вне, и внутри транзакции: это и есть смысл
		// прозрачной передачи транзакции через контекст.
		insert := func(ctx context.Context, email string) error {
			_, err := pool.Exec(ctx, `INSERT INTO users (name, email) VALUES ($1, $2)`, "User", email)

			return err
		}

		require.NoError(t, insert(ctx, "outside@example.com"))
		require.NoError(t, pool.InTx(ctx, func(ctx context.Context) error {
			return insert(ctx, "inside@example.com")
		}))

		count, err := Get[int64](ctx, pool, `SELECT count(*) FROM users`)
		require.NoError(t, err)
		assert.Equal(t, int64(2), count)
	})

	t.Run("nested InTx uses a savepoint", func(t *testing.T) {
		pool := newTestPool(t, cfg)
		ctx := t.Context()

		err := pool.InTx(ctx, func(ctx context.Context) error {
			if _, execErr := pool.Exec(ctx, `INSERT INTO users (name, email) VALUES ($1, $2)`, "Outer", "outer@example.com"); execErr != nil {
				return execErr
			}

			// Внутренний блок падает — откатывается только его часть.
			innerErr := pool.InTx(ctx, func(ctx context.Context) error {
				if _, execErr := pool.Exec(ctx, `INSERT INTO users (name, email) VALUES ($1, $2)`, "Inner", "inner@example.com"); execErr != nil {
					return execErr
				}

				return errors.New("inner failed")
			})
			require.Error(t, innerErr)

			// Внешняя транзакция жива и способна работать дальше — именно это
			// отличает точку сохранения от второй независимой транзакции.
			_, execErr := pool.Exec(ctx, `INSERT INTO users (name, email) VALUES ($1, $2)`, "After", "after@example.com")

			return execErr
		})
		require.NoError(t, err)

		emails, err := Select[string](ctx, pool, `SELECT email FROM users ORDER BY email`)
		require.NoError(t, err)
		assert.Equal(t, []string{"after@example.com", "outer@example.com"}, emails)
	})

	t.Run("serialization failure retries the whole transaction", func(t *testing.T) {
		pool := newTestPool(t, cfg)
		ctx := t.Context()

		_, err := pool.Exec(ctx, `INSERT INTO users (name, email) VALUES ('seed', 'seed@example.com')`)
		require.NoError(t, err)

		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			attempts int
		)

		// Две транзакции в SERIALIZABLE читают одно и то же и обе пишут: PostgreSQL
		// обязан отменить одну из них с 40001, а InTx — молча её перезапустить.
		worker := func(email string) {
			defer wg.Done()

			txErr := pool.InTx(ctx, func(ctx context.Context) error {
				mu.Lock()
				attempts++
				mu.Unlock()

				if _, scanErr := Get[int64](ctx, pool, `SELECT count(*) FROM users`); scanErr != nil {
					return scanErr
				}

				time.Sleep(100 * time.Millisecond)

				_, execErr := pool.Exec(ctx, `INSERT INTO users (name, email) VALUES ($1, $2)`, "racer", email)

				return execErr
			}, WithIsolationLevel(pgx.Serializable))

			assert.NoError(t, txErr, "конфликт сериализации должен быть погашен повтором")
		}

		wg.Add(2)

		go worker("racer1@example.com")
		go worker("racer2@example.com")

		wg.Wait()

		count, err := Get[int64](ctx, pool, `SELECT count(*) FROM users WHERE name = 'racer'`)
		require.NoError(t, err)
		assert.Equal(t, int64(2), count, "обе транзакции обязаны в итоге примениться")

		mu.Lock()
		defer mu.Unlock()
		assert.GreaterOrEqual(t, attempts, 3, "как минимум одна транзакция должна была перезапуститься")
	})

	t.Run("copy from", func(t *testing.T) {
		pool := newTestPool(t, cfg)
		ctx := t.Context()

		rows := []testUserRow{
			{Name: "u1", Email: "u1@example.com"},
			{Name: "u2", Email: "u2@example.com"},
			{Name: "u3", Email: "u3@example.com"},
		}

		copied, err := CopyFrom(ctx, pool, "users", []string{"name", "email"}, rows,
			func(u testUserRow) ([]any, error) { return []any{u.Name, u.Email}, nil })
		require.NoError(t, err)
		assert.Equal(t, int64(3), copied)

		count, err := Get[int64](ctx, pool, `SELECT count(*) FROM users`)
		require.NoError(t, err)
		assert.Equal(t, int64(3), count)

		empty, err := CopyFrom(ctx, pool, "users", []string{"name", "email"}, nil,
			func(u testUserRow) ([]any, error) { return nil, nil })
		require.NoError(t, err)
		assert.Zero(t, empty)
	})

	t.Run("bulk exec", func(t *testing.T) {
		pool := newTestPool(t, cfg)
		ctx := t.Context()

		args := [][]any{
			{"b1", "b1@example.com"},
			{"b2", "b2@example.com"},
		}

		require.NoError(t, BulkExec(ctx, pool, `INSERT INTO users (name, email) VALUES ($1, $2)`, args))

		count, err := Get[int64](ctx, pool, `SELECT count(*) FROM users`)
		require.NoError(t, err)
		assert.Equal(t, int64(2), count)

		// Ошибка внутри пачки должна указывать на конкретный элемент.
		err = BulkExec(ctx, pool, `INSERT INTO users (name, email) VALUES ($1, $2)`, [][]any{
			{"b3", "b3@example.com"},
			{"b4", "b1@example.com"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "batch item 1")
	})

	t.Run("query builder", func(t *testing.T) {
		pool := newTestPool(t, cfg)
		ctx := t.Context()

		_, err := ExecBy(ctx, pool, Builder().Insert("users").Columns("name", "email").Values("Ada", "sq@example.com"))
		require.NoError(t, err)

		user, err := GetBy[testUserRow](ctx, pool,
			Builder().Select("id", "name", "email").From("users").Where(map[string]any{"email": "sq@example.com"}))
		require.NoError(t, err)
		assert.Equal(t, "Ada", user.Name)

		all, err := SelectBy[testUserRow](ctx, pool, Builder().Select("id", "name", "email").From("users"))
		require.NoError(t, err)
		assert.Len(t, all, 1)
	})

	t.Run("statement timeout kills runaway queries", func(t *testing.T) {
		timeoutCfg := cfg
		timeoutCfg.StatementTimeout = 200 * time.Millisecond

		pool := newTestPool(t, timeoutCfg)

		_, err := pool.Exec(t.Context(), `SELECT pg_sleep(3)`)
		require.Error(t, err)
		assert.True(t, IsQueryCanceled(err), "сервер обязан сам прервать запрос, а не ждать клиента")
	})

	t.Run("advisory lock guards a critical section", func(t *testing.T) {
		pool := newTestPool(t, cfg)
		ctx := t.Context()

		key := AdvisoryKey("test.leader")

		lock, err := pool.TryAdvisoryLock(ctx, key)
		require.NoError(t, err)

		// Второй претендент не должен получить ту же блокировку.
		_, err = pool.TryAdvisoryLock(ctx, key)
		require.ErrorIs(t, err, ErrLockNotAcquired)

		require.NoError(t, lock.Unlock(ctx))
		require.NoError(t, lock.Unlock(ctx), "повторное освобождение безопасно")

		// После освобождения блокировка снова доступна.
		again, err := pool.TryAdvisoryLock(ctx, key)
		require.NoError(t, err)
		require.NoError(t, again.Unlock(ctx))

		executed := false
		require.NoError(t, pool.WithAdvisoryLock(ctx, key, func(context.Context) error {
			executed = true

			return nil
		}))
		assert.True(t, executed)
	})

	t.Run("read-only routing", func(t *testing.T) {
		pool := newTestPool(t, cfg)
		ctx := t.Context()

		assert.False(t, pool.HasReplicas())

		// Без настроенных реплик маршрутизация обязана сообщить об этом явно,
		// а не молча уйти на мастер.
		_, err := pool.Exec(ReadOnly(ctx), `SELECT 1`)
		require.ErrorIs(t, err, ErrNoReplicas)

		_, err = pool.ReplicationLag(ctx)
		require.ErrorIs(t, err, ErrNoReplicas)

		// Внутри транзакции пометка запрещена совсем.
		err = pool.InTx(ctx, func(txCtx context.Context) error {
			_, execErr := pool.Exec(ReadOnly(txCtx), `SELECT 1`)

			return execErr
		})
		require.ErrorIs(t, err, ErrReadOnlyInTx)

		require.ErrorIs(t, pool.InTx(ReadOnly(ctx), func(context.Context) error { return nil }), ErrReadOnlyInTx)
	})

	t.Run("listen and notify", func(t *testing.T) {
		pool := newTestPool(t, cfg)
		ctx := t.Context()

		listener, err := pool.Listen(ctx, "user_events")
		require.NoError(t, err)

		defer listener.Close()

		// Подписка устанавливается асинхронно — уведомление, отправленное до неё,
		// потеряется, поэтому повторяем до получения.
		deadline := time.After(15 * time.Second)

		for {
			require.NoError(t, pool.Notify(ctx, "user_events", "created"))

			select {
			case notification := <-listener.Notifications():
				assert.Equal(t, "user_events", notification.Channel)
				assert.Equal(t, "created", notification.Payload)

				return
			case <-time.After(200 * time.Millisecond):
			case <-deadline:
				t.Fatal("notification was not received")
			}
		}
	})

	t.Run("pool exposes statistics and closes idempotently", func(t *testing.T) {
		pool := newTestPool(t, cfg)

		stat := pool.Stat()
		require.NotNil(t, stat)
		assert.Positive(t, stat.MaxConns())

		pool.Close()
		pool.Close()
	})
}
