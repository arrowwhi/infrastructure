package telegram

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/arrowwhi/infrastructure/connections/postgres"
)

// DefaultPostgresTable — имя таблицы состояний по умолчанию.
const DefaultPostgresTable = "telegram_states"

var tableNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)?$`)

// PostgresStateSchema возвращает DDL таблицы — его удобно положить в миграцию сервиса.
// Состояние истекает по expires_at; удаляет истёкшие записи PostgresStore.Cleanup.
func PostgresStateSchema(table string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %[1]s (
    chat_id    bigint      NOT NULL,
    user_id    bigint      NOT NULL,
    name       text        NOT NULL,
    data       jsonb,
    expires_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, user_id)
);
CREATE INDEX IF NOT EXISTS %[2]s_expires_at_idx ON %[1]s (expires_at) WHERE expires_at IS NOT NULL;`, table, indexPrefix(table))
}

// PostgresStore — хранилище в таблице PostgreSQL.
type PostgresStore struct {
	pool  *postgres.Pool
	table string
}

// NewPostgresStore создаёт хранилище. table пустое — DefaultPostgresTable. Таблицу создаёт
// миграция сервиса (PostgresStateSchema) или EnsureSchema.
func NewPostgresStore(pool *postgres.Pool, table string) (*PostgresStore, error) {
	if table == "" {
		table = DefaultPostgresTable
	}

	if !tableNameRe.MatchString(table) {
		return nil, fmt.Errorf("state: invalid table name %q", table)
	}

	return &PostgresStore{pool: pool, table: table}, nil
}

// EnsureSchema создаёт таблицу, если её нет. Удобно для небольших ботов без миграций.
func (p *PostgresStore) EnsureSchema(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, PostgresStateSchema(p.table)); err != nil {
		return fmt.Errorf("state: create table: %w", err)
	}

	return nil
}

// Get возвращает состояние или ErrStateNotFound (в том числе для истёкшего).
func (p *PostgresStore) Get(ctx context.Context, key StateKey) (State, error) {
	var s State

	err := p.pool.QueryRow(ctx,
		`SELECT name, data FROM `+p.table+`
		 WHERE chat_id = $1 AND user_id = $2 AND (expires_at IS NULL OR expires_at > now())`,
		key.ChatID, key.UserID,
	).Scan(&s.Name, &s.Data)

	if errors.Is(err, pgx.ErrNoRows) {
		return State{}, ErrStateNotFound
	}

	if err != nil {
		return State{}, fmt.Errorf("state: get: %w", err)
	}

	return s, nil
}

// Set сохраняет состояние (upsert).
func (p *PostgresStore) Set(ctx context.Context, key StateKey, s State, ttl time.Duration) error {
	var expiresAt *time.Time

	if ttl > 0 {
		t := time.Now().Add(ttl)
		expiresAt = &t
	}

	var data any
	if len(s.Data) > 0 {
		data = []byte(s.Data)
	}

	_, err := p.pool.Exec(ctx,
		`INSERT INTO `+p.table+` (chat_id, user_id, name, data, expires_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, now())
		 ON CONFLICT (chat_id, user_id) DO UPDATE
		 SET name = EXCLUDED.name, data = EXCLUDED.data, expires_at = EXCLUDED.expires_at, updated_at = now()`,
		key.ChatID, key.UserID, s.Name, data, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("state: set: %w", err)
	}

	return nil
}

// Delete удаляет состояние.
func (p *PostgresStore) Delete(ctx context.Context, key StateKey) error {
	if _, err := p.pool.Exec(ctx,
		`DELETE FROM `+p.table+` WHERE chat_id = $1 AND user_id = $2`, key.ChatID, key.UserID); err != nil {
		return fmt.Errorf("state: delete: %w", err)
	}

	return nil
}

// Cleanup удаляет истёкшие записи и возвращает их число. Запускайте периодически
// (например, раз в час): Get и так их не возвращает, это только уборка места.
func (p *PostgresStore) Cleanup(ctx context.Context) (int64, error) {
	tag, err := p.pool.Exec(ctx, `DELETE FROM `+p.table+` WHERE expires_at IS NOT NULL AND expires_at <= now()`)
	if err != nil {
		return 0, fmt.Errorf("state: cleanup: %w", err)
	}

	return tag.RowsAffected(), nil
}

func indexPrefix(table string) string {
	out := []byte(table)
	for i, c := range out {
		if c == '.' {
			out[i] = '_'
		}
	}

	return string(out)
}
