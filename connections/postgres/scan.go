package postgres

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
)

// Get выполняет запрос и возвращает ровно одну строку.
//
// T может быть структурой (колонки сопоставляются с полями по имени, регистр не важен,
// имя переопределяется тегом `db`) либо скалярным типом. Если строк нет, возвращается
// ErrNoRows — проверяйте через postgres.IsNoRows.
func Get[T any](ctx context.Context, q Querier, sql string, args ...any) (T, error) {
	var zero T

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return zero, fmt.Errorf("query: %w", err)
	}

	result, err := pgx.CollectOneRow(rows, rowMapper[T]())
	if err != nil {
		return zero, err
	}

	return result, nil
}

// Select выполняет запрос и собирает все строки в срез.
// Пустой результат — не ошибка: возвращается пустой срез.
func Select[T any](ctx context.Context, q Querier, sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	result, err := pgx.CollectRows(rows, rowMapper[T]())
	if err != nil {
		return nil, fmt.Errorf("collect rows: %w", err)
	}

	return result, nil
}

// GetExactlyOne ведёт себя как Get, но считает ошибкой и вторую строку тоже.
// Полезно там, где несколько подходящих строк означают нарушение инварианта данных.
func GetExactlyOne[T any](ctx context.Context, q Querier, sql string, args ...any) (T, error) {
	var zero T

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return zero, fmt.Errorf("query: %w", err)
	}

	result, err := pgx.CollectExactlyOneRow(rows, rowMapper[T]())
	if err != nil {
		return zero, err
	}

	return result, nil
}

// Exists сообщает, вернул ли запрос хотя бы одну строку.
func Exists(ctx context.Context, q Querier, sql string, args ...any) (bool, error) {
	var exists bool

	if err := q.QueryRow(ctx, "SELECT EXISTS ("+sql+")", args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("query exists: %w", err)
	}

	return exists, nil
}

// rowMapper выбирает способ разбора строки по типу T.
//
// Структуры разбираются по именам колонок, остальное — как одиночное значение.
// time.Time исключён из структурного разбора намеренно: это скалярный тип PostgreSQL,
// хотя в Go и является структурой.
func rowMapper[T any]() pgx.RowToFunc[T] {
	typ := reflect.TypeFor[T]()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	if typ.Kind() == reflect.Struct && typ != reflect.TypeFor[time.Time]() {
		return pgx.RowToStructByNameLax[T]
	}

	return pgx.RowTo[T]
}
