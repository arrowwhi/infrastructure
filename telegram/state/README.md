# telegram/state

Хранилище шагов диалога Telegram-бота: имя шага (`"awaiting_title"`) плюс произвольные данные в JSON,
отдельно для каждого пользователя в каждом чате, со сроком жизни. Три реализации интерфейса `Store`:
в памяти (тесты, локальная разработка), PostgreSQL и Redis (переживают рестарт и работают при
нескольких копиях бота). Обычно используется через `bot.WithStateStore` и `bot.Context.SetState`.

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import "github.com/arrowwhi/infrastructure/telegram/state"
```

## Пример

```go
package main

import (
	"context"
	"log"

	"github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/arrowwhi/infrastructure/connections/redis"
	"github.com/arrowwhi/infrastructure/telegram/bot"
	"github.com/arrowwhi/infrastructure/telegram/state"
)

func newStore(ctx context.Context, kind string, pool *postgres.Pool, rdb *redis.Client) (state.Store, error) {
	switch kind {
	case "postgres":
		store, err := state.NewPostgres(pool, "") // таблица telegram_states
		if err != nil {
			return nil, err
		}
		return store, store.EnsureSchema(ctx) // в сервисе лучше миграцией: state.PostgresSchema("")
	case "redis":
		return state.NewRedis(rdb.Client, ""), nil // ключи telegram:state:<chat>:<user>
	default:
		return state.NewMemory(), nil
	}
}

func main() {
	ctx := context.Background()

	store, err := newStore(ctx, "memory", nil, nil)
	if err != nil {
		log.Fatal(err)
	}

	b, err := bot.New(bot.DefaultConfig("123:abc"), bot.WithStateStore(store))
	if err != nil {
		log.Fatal(err)
	}
	_ = b

	// Напрямую (например, из воркера):
	key := state.Key{ChatID: 42, UserID: 42}
	s, err := state.New("awaiting_title", map[string]string{"source": "worker"})
	if err != nil {
		log.Fatal(err)
	}
	if err = store.Set(ctx, key, s, 0); err != nil { // ttl <= 0 — без срока жизни
		log.Fatal(err)
	}
}
```

В обработчиках бота: `c.SetState(name, data)`, `c.State()` → `(state.State, ok, err)`,
`s.Decode(&v)`, `c.ClearState()`.

## Конфигурация

Переменных нет. Параметры конструкторов:

| Конструктор | Параметр | По умолчанию | |
|---|---|---|---|
| `state.NewPostgres(pool, table)` | `table` | `telegram_states` (`state.DefaultPostgresTable`) | |
| `state.NewRedis(client, prefix)` | `prefix` | `telegram:state:` (`state.DefaultRedisPrefix`) | любой `goredis.UniversalClient` |
| `state.NewMemory()` | — | — | |

Срок жизни задаёт бот: `TELEGRAM_STATE_TTL` (24h), см. [`telegram/bot`](../bot/README.md).

## Метрики, трейсы, логи

Своих нет. Запросы `NewPostgres` идут через `connections/postgres`, `NewRedis` — через переданный
клиент, поэтому попадают в их метрики и трейсы (и в трейс обновления бота).

## Связь с другими пакетами

- [`telegram/bot`](../bot/README.md): `bot.WithStateStore(store)`; без опции — `NewMemory`.
- [`connections/postgres`](../../connections/postgres/README.md): `NewPostgres` принимает `*postgres.Pool`;
  DDL — `state.PostgresSchema(table)` для своей миграции ([`migrate`](../../connections/postgres/migrate/README.md))
  или `store.EnsureSchema(ctx)`.
- [`connections/redis`](../../connections/redis/README.md): передавайте встроенный клиент — `rdb.Client`.

## Подводные камни

- `Memory` теряет состояния при рестарте и не разделяет их между копиями бота — только для тестов
  и локальной разработки.
- PostgreSQL не удаляет истёкшие строки сам: `Get` их не возвращает, но место освобождает только
  `store.Cleanup(ctx)` — запускайте периодически (например, раз в час). В Redis истечение — по TTL ключа.
- `Get` для отсутствующего или истёкшего состояния возвращает `state.ErrNotFound`
  (`c.State()` превращает его в `ok == false`).
- Данные хранятся как JSON: меняя структуру данных шага, помните о состояниях, сохранённых старой
  версией бота (до `TELEGRAM_STATE_TTL`).
