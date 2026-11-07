# utils/txmanager

Координатор одной логической операции сразу в PostgreSQL и Redis: открывает транзакцию PostgreSQL
и `MULTI/EXEC`-конвейер Redis, кладёт их в `context.Context`, а в конце сначала выполняет конвейер
Redis и только потом коммитит PostgreSQL — ошибка Redis успевает откатить базу. Для транзакций
только по PostgreSQL пакет не нужен: используйте `postgres.Pool.InTx` (точки сохранения, перезапуск
при конфликте сериализации, откат при панике).

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import "github.com/arrowwhi/infrastructure/utils/txmanager"
```

## Пример

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/arrowwhi/infrastructure/connections/redis"
	"github.com/arrowwhi/infrastructure/utils/txmanager"
)

type Service struct {
	tm   *txmanager.TxManager
	pool *postgres.Pool
}

// Rename обновляет имя в базе и сбрасывает кэш в одной логической операции.
func (s *Service) Rename(ctx context.Context, id int64, name string) (err error) {
	ctx, err = s.tm.Begin(ctx)
	if err != nil {
		return err
	}
	defer s.tm.CommitOrRollback(ctx, &err) // именованный err обязателен

	// pool сам подхватывает транзакцию из ctx.
	if _, err = s.pool.Exec(ctx, `UPDATE users SET name = $1 WHERE id = $2`, name, id); err != nil {
		return err
	}

	// Команда встаёт в конвейер и выполнится в CommitOrRollback.
	txmanager.GetRedisTx(ctx).Set(ctx, "user:name:42", name, time.Hour)

	return nil
}

func main() {
	ctx := context.Background()

	cfg := postgres.DefaultConfig()
	cfg.User, cfg.Database = "svc", "app"

	pool, err := postgres.New(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	rdb, err := redis.New(ctx, redis.Config{Addr: "localhost:6379"})
	if err != nil {
		log.Fatal(err)
	}
	defer rdb.Close()

	svc := &Service{tm: txmanager.New(pool, rdb.Client), pool: pool}
	if err = svc.Rename(ctx, 42, "Иван"); err != nil {
		log.Fatal(err)
	}
}
```

Только одно хранилище — `BeginPostgres(ctx)` или `BeginRedis(ctx)` вместо `Begin`; `CommitOrRollback`
завершает то, что есть в контексте.

## Конфигурация

Нет. `txmanager.New(pool, redisClient)` — любой аргумент может быть `nil`, тогда соответствующий
`Begin*` вернёт `ErrPostgresNotInitialized` / `ErrRedisNotInitialized`.

## Метрики, трейсы, логи

Своих нет: запросы идут через `connections/postgres` и клиент Redis и попадают в их метрики и трейсы.

## Связь с другими пакетами

- [`connections/postgres`](../../connections/postgres/README.md): транзакция открывается через
  `Pool.Begin`, поэтому `pool.Query/Exec`, `postgres.Get[T]` и т.п. с этим `ctx` работают внутри неё —
  доставать транзакцию из контекста в репозиториях не нужно (`GetPgTx` устарел).
- [`connections/redis`](../../connections/redis/README.md): передавайте встроенный клиент `rdb.Client`.
- Для кода на sqlc: `pgx.Tx` — `tx.Raw()` у `postgres.TxFromContext(ctx)`.

## Подводные камни

- Используйте именованный `err` и `defer CommitOrRollback(ctx, &err)`: решение «коммит или откат»
  принимается по значению ошибки.
- **Паника не откатывает транзакцию**: при панике `err` остаётся `nil`, и `CommitOrRollback` закоммитит
  частично выполненную работу. Не допускайте паник внутри или перехватывайте их сами.
- `Begin` требует оба хранилища: при `Redis == nil` он вернёт ошибку, а уже открытая транзакция
  PostgreSQL останется незакрытой (держит соединение до таймаута `DB_IDLE_IN_TX_SESSION_TIMEOUT`).
- Конвейер Redis — это `MULTI/EXEC`, а не транзакция с откатом: если `EXEC` прошёл, а `COMMIT`
  PostgreSQL упал, изменения в Redis останутся. Результаты команд конвейера до `EXEC` недоступны.
- Вложенный `Begin` с уже транзакционным `ctx` откроет в PostgreSQL `SAVEPOINT`, а в Redis — новый
  отдельный конвейер, который выполнится раньше внешнего. Не вкладывайте — пробрасывайте `ctx`.
