package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	defaultTxRetries        = 3
	defaultRetryInitial     = 20 * time.Millisecond
	defaultRetryMaxInterval = 500 * time.Millisecond
)

// TxFunc — тело транзакции. Контекст уже содержит транзакцию, поэтому вызовы
// репозиториев внутри автоматически попадают в неё.
type TxFunc func(ctx context.Context) error

// TxOption настраивает транзакцию.
type TxOption func(*txSettings)

// Tx — транзакция или точка сохранения внутри неё. Реализует Querier, поэтому
// код, работающий с пулом, работает и здесь.
type Tx struct {
	tx     pgx.Tx
	pool   *Pool
	nested bool
}

type txSettings struct {
	options    pgx.TxOptions
	maxRetries uint
}

type txCtxKey struct{}

// WithIsolationLevel задаёт уровень изоляции транзакции.
func WithIsolationLevel(level pgx.TxIsoLevel) TxOption {
	return func(s *txSettings) {
		s.options.IsoLevel = level
	}
}

// WithTxAccessMode задаёт режим доступа (например, pgx.ReadOnly для отчётов).
func WithTxAccessMode(mode pgx.TxAccessMode) TxOption {
	return func(s *txSettings) {
		s.options.AccessMode = mode
	}
}

// WithTxRetries задаёт число попыток выполнения транзакции при serialization failure
// и дедлоках. Ноль отключает повторы.
func WithTxRetries(attempts uint) TxOption {
	return func(s *txSettings) {
		s.maxRetries = attempts
	}
}

// InTx выполняет fn в транзакции, сам делая commit при успехе и rollback при ошибке.
//
// Если контекст уже содержит транзакцию, InTx не открывает вторую (это привело бы
// к взаимной блокировке на собственных же строках), а создаёт SAVEPOINT: ошибка
// внутреннего блока откатывает только его часть, коммитит по-прежнему внешний вызов.
//
// Транзакция целиком перезапускается при 40001 (serialization_failure) и 40P01
// (deadlock_detected). Повторы применяются только к внешней транзакции — перезапускать
// одну лишь точку сохранения бессмысленно, конфликт находится уровнем выше.
func (p *Pool) InTx(ctx context.Context, fn TxFunc, opts ...TxOption) error {
	if isReadOnlyRoute(ctx) {
		return ErrReadOnlyInTx
	}

	if parent := txFromContext(ctx); parent != nil {
		return parent.runSavepoint(ctx, fn)
	}

	settings := newTxSettings(opts)

	operation := func() (struct{}, error) {
		err := p.runTx(ctx, settings.options, fn)
		if err != nil && !IsRetryableTx(err) {
			return struct{}{}, backoff.Permanent(err)
		}

		return struct{}{}, err
	}

	_, err := backoff.Retry(ctx, operation,
		backoff.WithBackOff(newTxBackOff()),
		backoff.WithMaxTries(settings.maxRetries+1),
	)

	return err
}

// Begin открывает транзакцию и возвращает контекст с ней внутри.
// Низкоуровневое API для случаев, когда commit и rollback нужно контролировать вручную
// (например, координация с другим хранилищем). В обычном коде предпочтительнее InTx.
func (p *Pool) Begin(ctx context.Context, opts ...TxOption) (context.Context, *Tx, error) {
	if p.primary == nil {
		return ctx, nil, ErrClosed
	}

	if existing := txFromContext(ctx); existing != nil {
		sp, err := existing.savepoint(ctx)
		if err != nil {
			return ctx, nil, err
		}

		return contextWithTx(ctx, sp), sp, nil
	}

	settings := newTxSettings(opts)

	pgxTx, err := p.primary.BeginTx(ctx, settings.options)
	if err != nil {
		return ctx, nil, fmt.Errorf("begin tx: %w", err)
	}

	tx := &Tx{tx: pgxTx, pool: p}

	return contextWithTx(ctx, tx), tx, nil
}

// Commit фиксирует транзакцию либо освобождает точку сохранения.
func (t *Tx) Commit(ctx context.Context) error {
	if err := t.tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// Rollback откатывает транзакцию либо возвращает состояние к точке сохранения.
// Повторный вызов после Commit безопасен и ошибки не возвращает.
func (t *Tx) Rollback(ctx context.Context) error {
	if err := t.tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return fmt.Errorf("rollback: %w", err)
	}

	return nil
}

// Nested сообщает, что это точка сохранения внутри другой транзакции.
func (t *Tx) Nested() bool {
	return t.nested
}

// Raw возвращает нижележащую транзакцию pgx.
func (t *Tx) Raw() pgx.Tx {
	return t.tx
}

func (t *Tx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return t.tx.Query(ctx, sql, args...)
}

func (t *Tx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return t.tx.QueryRow(ctx, sql, args...)
}

func (t *Tx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return t.tx.Exec(ctx, sql, args...)
}

func (t *Tx) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	return t.tx.SendBatch(ctx, batch)
}

func (t *Tx) CopyFrom(ctx context.Context, table pgx.Identifier, columns []string, src pgx.CopyFromSource) (int64, error) {
	return t.tx.CopyFrom(ctx, table, columns, src)
}

// savepoint открывает точку сохранения внутри текущей транзакции.
func (t *Tx) savepoint(ctx context.Context) (*Tx, error) {
	sp, err := t.tx.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin savepoint: %w", err)
	}

	return &Tx{tx: sp, pool: t.pool, nested: true}, nil
}

// runSavepoint выполняет fn в точке сохранения.
func (t *Tx) runSavepoint(ctx context.Context, fn TxFunc) (err error) {
	sp, err := t.savepoint(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if r := recover(); r != nil {
			_ = sp.Rollback(ctx)

			panic(r)
		}

		if err != nil {
			if rbErr := sp.Rollback(ctx); rbErr != nil {
				err = errors.Join(err, rbErr)
			}
		}
	}()

	if err = fn(contextWithTx(ctx, sp)); err != nil {
		return err
	}

	return sp.Commit(ctx)
}

// runTx выполняет одну попытку транзакции.
func (p *Pool) runTx(ctx context.Context, options pgx.TxOptions, fn TxFunc) (err error) {
	if p.primary == nil {
		return ErrClosed
	}

	pgxTx, err := p.primary.BeginTx(ctx, options)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	tx := &Tx{tx: pgxTx, pool: p}

	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(ctx)

			panic(r)
		}

		if err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				err = errors.Join(err, rbErr)
			}
		}
	}()

	if err = fn(contextWithTx(ctx, tx)); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// newTxSettings собирает настройки транзакции из опций.
func newTxSettings(opts []TxOption) txSettings {
	settings := txSettings{maxRetries: defaultTxRetries}
	for _, opt := range opts {
		opt(&settings)
	}

	return settings
}

// newTxBackOff — экспоненциальная задержка с джиттером между попытками транзакции.
func newTxBackOff() backoff.BackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = defaultRetryInitial
	b.MaxInterval = defaultRetryMaxInterval

	return b
}

// contextWithTx кладёт транзакцию в контекст.
func contextWithTx(ctx context.Context, tx *Tx) context.Context {
	return context.WithValue(ctx, txCtxKey{}, tx)
}

// txFromContext достаёт транзакцию из контекста, если она там есть.
func txFromContext(ctx context.Context) *Tx {
	tx, _ := ctx.Value(txCtxKey{}).(*Tx)

	return tx
}

// TxFromContext возвращает текущую транзакцию и признак её наличия.
// Нужен там, где важно знать, идёт ли выполнение внутри транзакции.
func TxFromContext(ctx context.Context) (*Tx, bool) {
	tx := txFromContext(ctx)

	return tx, tx != nil
}
