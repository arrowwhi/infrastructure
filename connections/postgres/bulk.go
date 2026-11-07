package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// CopyFrom заливает срез строк протоколом COPY — самый быстрый способ массовой вставки,
// на больших объёмах он на порядок обгоняет обычные INSERT.
//
// Ограничение протокола: ON CONFLICT не поддерживается. Если нужен upsert, заливайте
// данные во временную таблицу через CopyFrom, а затем выполняйте
// INSERT ... SELECT ... ON CONFLICT DO UPDATE.
//
// Имя таблицы может быть со схемой: "public.users".
func CopyFrom[T any](
	ctx context.Context,
	q Querier,
	table string,
	columns []string,
	rows []T,
	toRow func(T) ([]any, error),
) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}

	source := pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
		values, err := toRow(rows[i])
		if err != nil {
			return nil, fmt.Errorf("build row %d: %w", i, err)
		}

		return values, nil
	})

	copied, err := q.CopyFrom(ctx, identifier(table), columns, source)
	if err != nil {
		return copied, fmt.Errorf("copy into %s: %w", table, err)
	}

	return copied, nil
}

// BulkExec выполняет один и тот же запрос с разными наборами аргументов за один
// сетевой обмен. Это лекарство от N+1: сто отдельных Exec — это сто round-trip'ов,
// один BulkExec — один.
//
// Повторов здесь нет: часть запросов пачки может успеть выполниться, поэтому
// безопасно перезапустить её целиком можно только внутри транзакции.
func BulkExec(ctx context.Context, q Querier, sql string, argsList [][]any) error {
	if len(argsList) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, args := range argsList {
		batch.Queue(sql, args...)
	}

	results := q.SendBatch(ctx, batch)

	var execErr error

	for i := range argsList {
		if _, err := results.Exec(); err != nil {
			execErr = fmt.Errorf("batch item %d: %w", i, err)

			break
		}
	}

	closeErr := results.Close()

	if execErr != nil {
		return execErr
	}

	if closeErr != nil {
		return fmt.Errorf("close batch: %w", closeErr)
	}

	return nil
}

// identifier разбирает имя таблицы вида "schema.table" в идентификатор pgx,
// который экранирует каждую часть отдельно.
func identifier(table string) pgx.Identifier {
	return pgx.Identifier(strings.Split(table, "."))
}
