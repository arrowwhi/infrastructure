// Package txmanager coordinates transactions that span PostgreSQL and Redis.
//
// Транзакции только по PostgreSQL этому пакету не нужны: их закрывает postgres.Pool.InTx,
// который умеет ещё и точки сохранения, и перезапуск при конфликте сериализации.
// TxManager остаётся ровно для одного случая — когда одну операцию нужно провести
// сразу в двух хранилищах.
package txmanager

import (
	"context"

	"github.com/pkg/errors"
	goredis "github.com/redis/go-redis/v9"

	"github.com/arrowwhi/infrastructure/connections/postgres"
)

type redisKey struct{}

// TxManager координирует транзакцию PostgreSQL и конвейер Redis.
type TxManager struct {
	PgPool *postgres.Pool
	Redis  *goredis.Client
}

// New создаёт координатор. Любое из хранилищ может быть nil — тогда работа
// с ним вернёт соответствующую ошибку.
func New(pg *postgres.Pool, r *goredis.Client) *TxManager {
	return &TxManager{PgPool: pg, Redis: r}
}

// BeginPostgres открывает транзакцию PostgreSQL и кладёт её в контекст.
// Запросы через postgres.Pool с этим контекстом попадут в неё автоматически.
func (tm *TxManager) BeginPostgres(ctx context.Context) (context.Context, error) {
	if tm.PgPool == nil {
		return ctx, ErrPostgresNotInitialized
	}

	txCtx, _, err := tm.PgPool.Begin(ctx)
	if err != nil {
		return ctx, errors.Wrap(err, "tm.PgPool.Begin")
	}

	return txCtx, nil
}

// BeginRedis открывает конвейер Redis и кладёт его в контекст.
func (tm *TxManager) BeginRedis(ctx context.Context) (context.Context, error) {
	if tm.Redis == nil {
		return ctx, ErrRedisNotInitialized
	}

	return context.WithValue(ctx, redisKey{}, tm.Redis.TxPipeline()), nil
}

// Begin открывает транзакцию в обоих хранилищах.
func (tm *TxManager) Begin(ctx context.Context) (context.Context, error) {
	ctxNew, err := tm.BeginPostgres(ctx)
	if err != nil {
		return ctx, errors.Wrap(err, "tm.BeginPostgres")
	}

	ctxNew, err = tm.BeginRedis(ctxNew)
	if err != nil {
		return ctx, errors.Wrap(err, "tm.BeginRedis")
	}

	return ctxNew, nil
}

// CommitOrRollback завершает обе транзакции. Вызывается через defer с именованной
// переменной ошибки: значение по указателю определяет, коммитить или откатывать.
//
// Порядок важен: сначала выполняется конвейер Redis, и только потом коммитится
// PostgreSQL. Ошибка Redis успевает откатить PostgreSQL, обратный порядок такой
// возможности не оставлял бы.
func (tm *TxManager) CommitOrRollback(ctx context.Context, errPtr *error) {
	pgTx, hasPgTx := postgres.TxFromContext(ctx)
	redisTx := GetRedisTx(ctx)

	// Откат выполняется в отдельном контексте: исходный к этому моменту может быть
	// уже отменён, и тогда откат не дошёл бы до базы.
	rollbackCtx := context.WithoutCancel(ctx)

	if *errPtr != nil {
		if hasPgTx {
			_ = pgTx.Rollback(rollbackCtx)
		}

		return
	}

	if redisTx != nil {
		if _, err := redisTx.Exec(ctx); err != nil {
			if hasPgTx {
				_ = pgTx.Rollback(rollbackCtx)
			}

			*errPtr = errors.Wrap(err, "redisTx.Exec")

			return
		}
	}

	if hasPgTx {
		if err := pgTx.Commit(ctx); err != nil {
			*errPtr = errors.Wrap(err, "pgTx.Commit")
		}
	}
}

// GetPgTx возвращает транзакцию PostgreSQL из контекста.
//
// Deprecated: репозиториям это больше не нужно — postgres.Pool сам подхватывает
// транзакцию из контекста, поэтому один и тот же код работает и внутри, и снаружи её.
func GetPgTx(ctx context.Context) *postgres.Tx {
	tx, _ := postgres.TxFromContext(ctx)

	return tx
}

// GetRedisTx возвращает конвейер Redis из контекста.
func GetRedisTx(ctx context.Context) goredis.Pipeliner {
	pipe, _ := ctx.Value(redisKey{}).(goredis.Pipeliner)

	return pipe
}
