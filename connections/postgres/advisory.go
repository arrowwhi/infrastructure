package postgres

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrLockNotAcquired возвращается, когда блокировку удерживает кто-то другой.
var ErrLockNotAcquired = errors.New("postgres: advisory lock is held by another session")

// AdvisoryLock — удерживаемая сессионная блокировка PostgreSQL.
//
// Блокировка привязана к соединению, поэтому на время удержания соединение изымается
// из пула и возвращается только в Unlock. Не удерживайте её дольше необходимого:
// это соединение, которого не хватит остальным запросам.
type AdvisoryLock struct {
	conn *pgxpool.Conn
	key  int64
}

// AdvisoryKey превращает строку в числовой ключ блокировки.
// PostgreSQL принимает только числа, а осмысленное имя удобнее хранить в коде.
func AdvisoryKey(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))

	return int64(h.Sum64()) //nolint:gosec // переполнение допустимо: нужен любой стабильный ключ
}

// TryAdvisoryLock пытается взять блокировку, не дожидаясь освобождения.
// Возвращает ErrLockNotAcquired, если её держит другая сессия.
//
// Основное применение — выбор лидера: несколько экземпляров сервиса стартуют
// периодическую задачу, но выполняет её тот, кто первым взял блокировку.
// Это избавляет от отдельного Redis или etcd ради одного мьютекса.
func (p *Pool) TryAdvisoryLock(ctx context.Context, key int64) (*AdvisoryLock, error) {
	conn, err := p.acquireForLock(ctx)
	if err != nil {
		return nil, err
	}

	var acquired bool

	if err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired); err != nil {
		conn.Release()

		return nil, fmt.Errorf("try advisory lock: %w", err)
	}

	if !acquired {
		conn.Release()

		return nil, ErrLockNotAcquired
	}

	return &AdvisoryLock{conn: conn, key: key}, nil
}

// AdvisoryLock берёт блокировку, дожидаясь её освобождения.
// Ожидание прерывается отменой контекста.
func (p *Pool) AdvisoryLock(ctx context.Context, key int64) (*AdvisoryLock, error) {
	conn, err := p.acquireForLock(ctx)
	if err != nil {
		return nil, err
	}

	if _, err = conn.Exec(ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
		conn.Release()

		return nil, fmt.Errorf("advisory lock: %w", err)
	}

	return &AdvisoryLock{conn: conn, key: key}, nil
}

// WithAdvisoryLock выполняет fn под блокировкой и освобождает её в любом случае.
// Если блокировка занята, fn не вызывается и возвращается ErrLockNotAcquired.
func (p *Pool) WithAdvisoryLock(ctx context.Context, key int64, fn func(context.Context) error) error {
	lock, err := p.TryAdvisoryLock(ctx, key)
	if err != nil {
		return err
	}

	defer func() {
		if unlockErr := lock.Unlock(ctx); unlockErr != nil {
			// Соединение уже возвращено в пул: сессионная блокировка снимется
			// при его закрытии, поэтому здесь достаточно сообщить об ошибке.
			_ = unlockErr
		}
	}()

	return fn(ctx)
}

// AdvisoryXactLock берёт блокировку на время транзакции. Освобождать её вручную
// не нужно: PostgreSQL снимет её при commit или rollback.
// Требует активной транзакции в контексте.
func AdvisoryXactLock(ctx context.Context, q Querier, key int64) error {
	if txFromContext(ctx) == nil {
		return fmt.Errorf("%w: advisory xact lock requires an active transaction", ErrInvalidConfig)
	}

	if _, err := q.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", key); err != nil {
		return fmt.Errorf("advisory xact lock: %w", err)
	}

	return nil
}

// Key возвращает ключ удерживаемой блокировки.
func (l *AdvisoryLock) Key() int64 {
	return l.key
}

// Unlock освобождает блокировку и возвращает соединение в пул.
// Повторный вызов безопасен.
func (l *AdvisoryLock) Unlock(ctx context.Context) error {
	if l.conn == nil {
		return nil
	}

	conn := l.conn
	l.conn = nil

	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", l.key); err != nil {
		return fmt.Errorf("advisory unlock: %w", err)
	}

	return nil
}

// acquireForLock изымает соединение из пула мастера под блокировку.
func (p *Pool) acquireForLock(ctx context.Context) (*pgxpool.Conn, error) {
	if p.primary == nil {
		return nil, ErrClosed
	}

	conn, err := p.primary.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}

	return conn, nil
}
