// Package migrate applies SQL migrations with goose over an existing postgres.Pool.
//
// Подпакет отдельный намеренно: сервисы, которые прогоняют миграции своим способом
// (отдельной job в CI, Liquibase, чем угодно), не импортируют его и не платят
// за зависимость goose.
//
// Миграции защищены advisory-блокировкой, поэтому одновременный старт нескольких
// экземпляров сервиса безопасен: миграции применит ровно один, остальные подождут.
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/arrowwhi/infrastructure/connections/postgres"
)

// lockName — имя advisory-блокировки, под которой выполняются миграции.
const lockName = "postgres.migrations"

// Up применяет все неприменённые миграции из fsys.
//
// dir — путь к каталогу с миграциями внутри fsys ("." для корня embed.FS).
// Пример:
//
//	//go:embed migrations/*.sql
//	var migrations embed.FS
//
//	err := migrate.Up(ctx, pool, migrations, "migrations")
func Up(ctx context.Context, pool *postgres.Pool, fsys fs.FS, dir string) error {
	return withLock(ctx, pool, func(db *sql.DB) error {
		if err := goose.UpContext(ctx, db, dir); err != nil {
			return fmt.Errorf("goose up: %w", err)
		}

		return nil
	}, fsys)
}

// UpTo применяет миграции до указанной версии включительно.
func UpTo(ctx context.Context, pool *postgres.Pool, fsys fs.FS, dir string, version int64) error {
	return withLock(ctx, pool, func(db *sql.DB) error {
		if err := goose.UpToContext(ctx, db, dir, version); err != nil {
			return fmt.Errorf("goose up-to %d: %w", version, err)
		}

		return nil
	}, fsys)
}

// Down откатывает последнюю применённую миграцию.
func Down(ctx context.Context, pool *postgres.Pool, fsys fs.FS, dir string) error {
	return withLock(ctx, pool, func(db *sql.DB) error {
		if err := goose.DownContext(ctx, db, dir); err != nil {
			return fmt.Errorf("goose down: %w", err)
		}

		return nil
	}, fsys)
}

// Version возвращает текущую версию схемы.
func Version(ctx context.Context, pool *postgres.Pool, fsys fs.FS, dir string) (int64, error) {
	var version int64

	err := withLock(ctx, pool, func(db *sql.DB) error {
		current, verErr := goose.GetDBVersionContext(ctx, db)
		if verErr != nil {
			return fmt.Errorf("goose version: %w", verErr)
		}

		version = current

		return nil
	}, fsys)

	return version, err
}

// withLock открывает database/sql поверх пула pgx и выполняет операцию goose
// под advisory-блокировкой.
func withLock(ctx context.Context, pool *postgres.Pool, fn func(*sql.DB) error, fsys fs.FS) error {
	if pool == nil {
		return postgres.ErrClosed
	}

	goose.SetBaseFS(fsys)

	if err := goose.SetDialect(string(goose.DialectPostgres)); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	// goose работает поверх database/sql, а у нас пул pgx: stdlib отдаёт совместимую
	// обёртку без второго набора соединений к базе.
	db := stdlib.OpenDBFromPool(pool.Raw())
	defer func() { _ = db.Close() }()

	return pool.WithAdvisoryLock(ctx, postgres.AdvisoryKey(lockName), func(context.Context) error {
		return fn(db)
	})
}
