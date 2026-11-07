## TxManager — единый менеджер транзакций (Postgres + Redis)

Коротко: даёт удобный способ запускать бизнес-операции в одной «логической» транзакции сразу для Postgres и Redis. В коде вы работаете только с `context.Context` и `txmanager.TxManager`.

### Установка

```bash
go get github.com/arrowwhi/infrastructure/utils/txmanager
```

### Как это работает

- `Begin(ctx)` — кладёт в контекст `pgx.Tx` и `redis.Pipeliner` (если подключены).
- `CommitOrRollback(ctx, &err)` — на основе `err` коммитит или откатывает:
  - если был Redis pipeline — сначала `Exec()`, затем `pgx.Commit()`;
  - при любой ошибке — `pgx.Rollback()`.
- В репозиториях вы просто читаете из контекста активную транзакцию.

---

# Postgres

### Объявить менеджер транзакций и прокинуть в сервисный слой

```go
type Service struct {
    TxManager *txmanager.TxManager
    Repo      *Repository
}
```

### Репозиторий с sqlc: обёртка для работы в транзакции

Добавьте хелпер, который «переключит» `sqlc.Queries` на `pgx.Tx`, если транзакция есть в контексте.

```go
// postgres.go
package postgres

import (
    "context"
    "github.com/arrowwhi/infrastructure/utils/txmanager"
    "your/module/internal/sqlc" // сгенерированный sqlc пакет
)

func InTx(ctx context.Context, queries *sqlc.Queries) *sqlc.Queries {
    if tx := txmanager.GetPgTx(ctx); tx != nil {
        return queries.WithTx(tx)
    }
    return queries
}
```

Использование в методах репозитория:

```go
type Repository struct {
    queries *sqlc.Queries
}

func (r *Repository) Create(ctx context.Context, input any) error {
    q := postgres.InTx(ctx, r.queries)
    return q.Create(ctx, input)
}
```

Если транзакция есть — вызовы пойдут внутри неё; если нет — будут выполнены отдельными запросами.

### Сервис: несколько операций в одной транзакции

```go
func (s *Service) Create(ctx context.Context, data any) (err error) {
    ctx, err = s.TxManager.BeginPostgres(ctx)
    if err != nil {
        return err
    }
    defer s.TxManager.CommitOrRollback(ctx, &err)

    if err = s.Repo.Create(ctx, data); err != nil {
        return err
    }

    // любые дополнительные операции, видящие ту же транзакцию через ctx
    // ...

    return nil
}
```

Нюанс: возвращайте именованный `err`, чтобы `defer` мог корректно закоммитить/откатить.

---

## Redis (необязательно)

Если при инициализации передан `*redis.Client`, `Begin(ctx)` положит в контекст `redis.Pipeliner`. Коммит/откат уже покрыт `CommitOrRollback`.

Как использовать pipeline в репозитории:

```go
import (
    "context"
    "github.com/arrowwhi/infrastructure/utils/txmanager"
)

type CacheRepo struct { /* ... */ }

func (r *CacheRepo) SetUser(ctx context.Context, key string, val string) error {
    if p := txmanager.GetRedisTx(ctx); p != nil {
        p.Set(ctx, key, val, 0) // попадает в pipeline; выполнится в CommitOrRollback
        return nil
    }
    // без транзакции — обычный клиент
    // r.client.Set(ctx, key, val, 0)
    return nil
}
```

Порядок коммита: сначала Redis pipeline (`Exec()`), затем Postgres (`Commit()`). Если `Exec()` упал — будет `Rollback()` Postgres и ошибка вверх.

---

## Обработка ошибок и best practices

- Всегда используйте именованный `err` в сервисных методах с `defer CommitOrRollback`.
- Не создавайте вложенные транзакции — просто пробрасывайте `ctx` глубже по вызовам.
- Репозитории должны быть «тупыми»: брать транзакцию из `ctx` и работать через неё, если она есть.
- Если `TxManager` инициализирован не полностью, `BeginPostgres/BeginRedis` вернут осмысленную ошибку (см. `errors.go`).

---

## Мини‑FAQ

- Можно ли использовать только Postgres или только Redis? — Да. Передайте `nil` вместо ненужного клиента, соответствующая часть просто не активируется.
- Что если я забуду `defer CommitOrRollback`? — Postgres транзакция «повиснет». Дисциплина: всегда шаблон с именованным `err`.
- sqlx/pgx без sqlc? — Логика та же: возьмите `pgx.Tx` из контекста и используйте его API.

---

## Быстрый шаблон для сервисного метода

```go
func (s *Service) Do(ctx context.Context, in Input) (err error) {
    ctx, err = s.TxManager.Begin(ctx)
    if err != nil { return err }
    defer s.TxManager.CommitOrRollback(ctx, &err)

    // repo calls ...
    return nil
}
```


