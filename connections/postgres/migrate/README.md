# connections/postgres/migrate

SQL-миграции [goose](https://github.com/pressly/goose) поверх уже созданного `postgres.Pool`:
файлы берутся из `fs.FS` (обычно `embed.FS` сервиса), выполнение защищено advisory-блокировкой
PostgreSQL. Подпакет отдельный намеренно: кто запускает миграции иначе (job в CI, отдельный бинарь),
не импортирует его и не тянет зависимость goose.

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import "github.com/arrowwhi/infrastructure/connections/postgres/migrate"
```

## Пример

```go
package main

import (
	"context"
	"embed"
	"log"

	"github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/arrowwhi/infrastructure/connections/postgres/migrate"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	ctx := context.Background()

	pool, err := postgres.New(ctx, postgres.DefaultConfig())
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	if err = migrate.Up(ctx, pool, migrations, "migrations"); err != nil {
		log.Fatal(err)
	}

	version, err := migrate.Version(ctx, pool, migrations, "migrations")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("schema version: %d", version)
}
```

Файлы — обычный формат goose:

```sql
-- migrations/00001_create_users.sql
-- +goose Up
CREATE TABLE users (id bigserial PRIMARY KEY, name text NOT NULL);

-- +goose Down
DROP TABLE users;
```

## API

| Функция | Действие |
|---|---|
| `Up(ctx, pool, fsys, dir)` | применить все неприменённые миграции |
| `UpTo(ctx, pool, fsys, dir, version)` | применить до версии включительно |
| `Down(ctx, pool, fsys, dir)` | откатить последнюю миграцию |
| `Version(ctx, pool, fsys, dir)` | текущая версия схемы |

`dir` — путь внутри `fsys` (`"."` для корня).

## Конфигурация

Своих настроек нет: подключение и таймауты — из `postgres.Config` пула. Таблица версий — стандартная
`goose_db_version`.

## Метрики, трейсы, логи

Запросы миграций идут через `database/sql`-обёртку над пулом pgx и не проходят через трейсер
`connections/postgres`. goose пишет свой лог применения стандартным `log` (stderr), мимо `observability/logger`.

## Связь с другими пакетами

- Требует `*postgres.Pool` из [`connections/postgres`](../README.md); второй набор соединений не создаётся.
- Для DDL таблицы состояний бота используйте `state.PostgresSchema(table)` из `telegram/state` в
  своей миграции.

## Подводные камни

- Блокировка берётся через `Pool.WithAdvisoryLock`, то есть **без ожидания**: если миграции уже
  выполняет другой экземпляр, `Up` сразу вернёт `postgres.ErrLockNotAcquired`. При одновременном
  старте нескольких подов обрабатывайте эту ошибку (повторить через паузу) или запускайте миграции
  отдельным шагом деплоя.
- goose хранит файловую систему и диалект в глобальном состоянии: не вызывайте функции пакета
  параллельно с разными `fsys`.
- `statement_timeout` пула (`DB_STATEMENT_TIMEOUT`) действует и на миграции: долгий `CREATE INDEX`
  может быть прерван.
