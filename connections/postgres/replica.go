package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// readOnlyCtxKey помечает контекст как пригодный для чтения с реплики.
type readOnlyCtxKey struct{}

// errBatchResults — pgx.BatchResults, отдающий заранее известную ошибку.
type errBatchResults struct {
	err error
}

var _ pgx.BatchResults = errBatchResults{}

// ReadOnly помечает контекст как читающий: запросы уйдут в пул реплик, если он настроен.
//
// Важно про репликационный лаг: реплика отстаёт от мастера, поэтому чтение сразу после
// записи может не увидеть только что записанное. Не используйте ReadOnly в сценариях
// «записал и тут же прочитал» — берите мастер.
//
// Внутри транзакции пометка запрещена: транзакция обязана целиком выполняться на мастере,
// поэтому такой вызов вернёт ErrReadOnlyInTx, а не уведёт запрос на реплику молча.
func ReadOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, readOnlyCtxKey{}, true)
}

// isReadOnlyRoute сообщает, помечен ли контекст как читающий.
func isReadOnlyRoute(ctx context.Context) bool {
	flag, _ := ctx.Value(readOnlyCtxKey{}).(bool)

	return flag
}

// HasReplicas сообщает, настроен ли отдельный пул реплик.
func (p *Pool) HasReplicas() bool {
	return p.replicas != nil
}

// route выбирает пул под запрос: реплики для помеченного чтения, иначе мастер.
func (p *Pool) route(ctx context.Context) (*pgxpool.Pool, error) {
	if p.primary == nil {
		return nil, ErrClosed
	}

	if isReadOnlyRoute(ctx) {
		if p.replicas == nil {
			return nil, ErrNoReplicas
		}

		return p.replicas, nil
	}

	return p.primary, nil
}

// txFor возвращает транзакцию из контекста, если она есть, и заодно проверяет,
// что чтение с реплики не запрашивают внутри транзакции.
func (p *Pool) txFor(ctx context.Context) (*Tx, error) {
	tx := txFromContext(ctx)
	if tx != nil && isReadOnlyRoute(ctx) {
		return nil, ErrReadOnlyInTx
	}

	return tx, nil
}

func (e errBatchResults) Exec() (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, e.err
}

func (e errBatchResults) Query() (pgx.Rows, error) {
	return nil, e.err
}

func (e errBatchResults) QueryRow() pgx.Row {
	return errRow{err: e.err}
}

func (e errBatchResults) Close() error {
	return e.err
}
