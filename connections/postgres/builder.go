package postgres

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/jackc/pgx/v5/pgconn"
)

// builder — эталонная настройка squirrel под PostgreSQL.
//
// Формат плейсхолдеров зафиксирован на $1, $2, ... : squirrel по умолчанию генерирует
// вопросительные знаки, и забытый PlaceholderFormat даёт ошибку только в рантайме.
var builder = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

// Builder возвращает билдер запросов, преднастроенный под PostgreSQL.
//
// Это функция, а не переменная, намеренно: экспортированную переменную любой
// потребитель мог бы переприсвоить и сломать формат плейсхолдеров сразу для всех
// в процессе. StatementBuilderType — значение, поэтому каждый вызов отдаёт
// независимую копию, и настройки одного вызывающего не текут к другому.
//
// RunWith использовать не нужно: запросы должен выполнять пул коннектора, иначе
// они пройдут мимо трейсинга, метрик и повторов.
func Builder() sq.StatementBuilderType {
	return builder
}

// GetBy выполняет собранный билдером запрос и возвращает одну строку.
func GetBy[T any](ctx context.Context, q Querier, builder sq.Sqlizer) (T, error) {
	var zero T

	sql, args, err := builder.ToSql()
	if err != nil {
		return zero, fmt.Errorf("build query: %w", err)
	}

	return Get[T](ctx, q, sql, args...)
}

// SelectBy выполняет собранный билдером запрос и возвращает все строки.
func SelectBy[T any](ctx context.Context, q Querier, builder sq.Sqlizer) ([]T, error) {
	sql, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	return Select[T](ctx, q, sql, args...)
}

// ExecBy выполняет собранный билдером запрос без возврата строк.
func ExecBy(ctx context.Context, q Querier, builder sq.Sqlizer) (pgconn.CommandTag, error) {
	sql, args, err := builder.ToSql()
	if err != nil {
		return pgconn.CommandTag{}, fmt.Errorf("build query: %w", err)
	}

	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return tag, fmt.Errorf("exec: %w", err)
	}

	return tag, nil
}
