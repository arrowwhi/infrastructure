package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier — общий интерфейс пула и транзакции. Репозиторий, принимающий Querier,
// одинаково работает и с пулом, и внутри транзакции.
//
// Набор методов совместим с интерфейсом DBTX, который генерирует sqlc, — если
// когда-нибудь понадобится кодогенерация, коннектор подойдёт как есть.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults
	CopyFrom(ctx context.Context, table pgx.Identifier, columns []string, src pgx.CopyFromSource) (int64, error)
}

// errRow — pgx.Row, отдающий заранее известную ошибку. Нужен, чтобы QueryRow сохранял
// сигнатуру pgx (ошибка всплывает на Scan), даже когда запрос не удалось выполнить.
type errRow struct {
	err error
}

// poolRow повторяет поведение pgx.QueryRow поверх уже полученных Rows.
// Своя реализация нужна, чтобы QueryRow проходил через тот же путь с повторами,
// что Query, — pgx.Conn.QueryRow повторов не делает.
type poolRow struct {
	rows pgx.Rows
}

// Compile-time проверки: и пул, и транзакция обязаны удовлетворять Querier.
var (
	_ Querier = (*Pool)(nil)
	_ Querier = (*Tx)(nil)
	_ pgx.Row = errRow{}
	_ pgx.Row = poolRow{}
)

func (r errRow) Scan(...any) error {
	return r.err
}

func (r poolRow) Scan(dest ...any) error {
	defer r.rows.Close()

	if !r.rows.Next() {
		if err := r.rows.Err(); err != nil {
			return err
		}

		return pgx.ErrNoRows
	}

	if err := r.rows.Scan(dest...); err != nil {
		return err
	}

	r.rows.Close()

	return r.rows.Err()
}

// Query выполняет запрос. Если в контексте есть транзакция — запрос идёт в неё,
// иначе выбирается пул мастера либо реплик (см. ReadOnly).
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx, err := p.txFor(ctx)
	if err != nil {
		return nil, err
	}

	if tx != nil {
		return tx.Query(ctx, sql, args...)
	}

	pool, err := p.route(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil && p.shouldRetry(err) {
		return pool.Query(ctx, sql, args...)
	}

	return rows, err
}

// QueryRow выполняет запрос, возвращающий не более одной строки.
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tx, err := p.txFor(ctx)
	if err != nil {
		return errRow{err: err}
	}

	if tx != nil {
		return tx.QueryRow(ctx, sql, args...)
	}

	rows, err := p.Query(ctx, sql, args...)
	if err != nil {
		return errRow{err: err}
	}

	return poolRow{rows: rows}
}

// Exec выполняет запрос без возврата строк.
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx, err := p.txFor(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}

	if tx != nil {
		return tx.Exec(ctx, sql, args...)
	}

	pool, err := p.route(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}

	tag, err := pool.Exec(ctx, sql, args...)
	if err != nil && p.shouldRetry(err) {
		return pool.Exec(ctx, sql, args...)
	}

	return tag, err
}

// SendBatch отправляет пачку запросов за один сетевой обмен.
// Повторов здесь нет: часть запросов пачки может успеть выполниться.
func (p *Pool) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	tx, err := p.txFor(ctx)
	if err != nil {
		return errBatchResults{err: err}
	}

	if tx != nil {
		return tx.SendBatch(ctx, batch)
	}

	pool, err := p.route(ctx)
	if err != nil {
		return errBatchResults{err: err}
	}

	return pool.SendBatch(ctx, batch)
}

// CopyFrom заливает строки протоколом COPY — самый быстрый способ массовой вставки.
func (p *Pool) CopyFrom(ctx context.Context, table pgx.Identifier, columns []string, src pgx.CopyFromSource) (int64, error) {
	tx, err := p.txFor(ctx)
	if err != nil {
		return 0, err
	}

	if tx != nil {
		return tx.CopyFrom(ctx, table, columns, src)
	}

	// COPY всегда идёт на мастер: это запись.
	if p.primary == nil {
		return 0, ErrClosed
	}

	return p.primary.CopyFrom(ctx, table, columns, src)
}

// shouldRetry решает, можно ли безопасно повторить запрос.
// По умолчанию — только когда pgx гарантирует, что запрос не дошёл до сервера.
func (p *Pool) shouldRetry(err error) bool {
	if pgconn.SafeToRetry(err) {
		return true
	}

	if p.retryPolicy != nil {
		return p.retryPolicy(err)
	}

	return false
}
