# connections/redis

Клиент Redis поверх [go-redis v9](https://github.com/redis/go-redis) с инструментацией
[redisotel](https://github.com/redis/go-redis/tree/master/extra/redisotel) (спан на команду,
метрики команд и пула), проверкой соединения (`PING`) при создании и `Healthcheck` для
`observability/health`. `*redis.Client` встроен, поэтому доступен весь API go-redis.

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import "github.com/arrowwhi/infrastructure/connections/redis"
```

## Пример

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/arrowwhi/infrastructure/connections/redis"
)

func main() {
	ctx := context.Background()

	rdb, err := redis.New(ctx, redis.Config{
		Addr:           "localhost:6379",
		DialTimeout:    5 * time.Second,
		ReadTimeout:    3 * time.Second,
		WriteTimeout:   3 * time.Second,
		PoolTimeout:    4 * time.Second,
		TracingEnabled: true,
		MetricsEnabled: true,
	})
	if err != nil {
		log.Fatal(err) // в том числе, если PING не прошёл
	}
	defer rdb.Close()

	if err = rdb.Set(ctx, "greeting", "hello", time.Minute).Err(); err != nil {
		log.Fatal(err)
	}

	val, err := rdb.Get(ctx, "greeting").Result()
	if err != nil {
		log.Fatal(err)
	}
	log.Println(val)
}
```

## Конфигурация

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `REDIS_ADDR` | `localhost:6379` | `host:port`; пустой — ошибка `New` |
| `REDIS_USERNAME`, `REDIS_PASSWORD` | — | ACL-пользователь и пароль |
| `REDIS_DB` | `0` | номер базы |
| `REDIS_POOL_SIZE` | `0` | размер пула; `0` — 10 × GOMAXPROCS |
| `REDIS_MIN_IDLE_CONNS` | `0` | минимум простаивающих соединений |
| `REDIS_DIAL_TIMEOUT` | `5s` | установка соединения |
| `REDIS_READ_TIMEOUT`, `REDIS_WRITE_TIMEOUT` | `3s`, `3s` | чтение/запись команды |
| `REDIS_POOL_TIMEOUT` | `4s` | ожидание свободного соединения |
| `REDIS_TLS_ENABLED` | `false` | TLS (минимум 1.2, системные корневые сертификаты) |
| `REDIS_TRACING_ENABLED` | `true` | спан на команду/конвейер |
| `REDIS_METRICS_ENABLED` | `true` | метрики `db.client.*` |

`redis.WithTracerProvider(tp)` / `redis.WithMeterProvider(mp)` — свои провайдеры вместо глобальных.

## Метрики, трейсы, логи

- Трейсы redisotel: спан на каждую команду и конвейер, атрибут `db.statement` — команда **вместе с
  аргументами**.
- Метрики redisotel (имена и единицы — как в redisotel, не приведены к секундам):
  `db.client.connections.create_time`, `db.client.connections.use_time` (мс),
  `db.client.connections.waits_duration` (нс), `db.client.connections.usage`, `.idle.max`, `.idle.min`,
  `.max`, `.waits`, `.timeouts`, `.hits`, `.misses`.
- Логов пакет не пишет.

## Связь с другими пакетами

- Health: `a.Health().Register("redis", rdb.Healthcheck)` — `PING` через пул, поэтому исчерпанный
  пул тоже валит проверку (по `REDIS_POOL_TIMEOUT`). Для кэша обычно `health.NonCritical()`.
- Остановка: `a.OnShutdown(func(context.Context) error { return rdb.Close() })`.
- `telegram/state.NewRedis(rdb.Client, "")` и `utils/txmanager.New(pool, rdb.Client)` принимают
  встроенный `*goredis.Client` — передавайте `rdb.Client`.

## Подводные камни

- `New` падает, если Redis недоступен при старте (проверка `PING`); переподключения после старта
  go-redis делает сам.
- `db.statement` в спанах содержит значения (`SET user:42 <payload>`): если в Redis лежат
  персональные данные, выключите трейсинг (`REDIS_TRACING_ENABLED=false`). В отличие от
  `connections/postgres`, аргументы здесь не вырезаются.
- Трейсинг и метрики включены по умолчанию (в `connections/postgres` — выключены).
- `env-default` применяется только при загрузке через cleanenv; `Config` литералом получает нулевые
  таймауты — go-redis подставит свои значения по умолчанию.
- Cluster и Sentinel не поддерживаются — только одиночный адрес.
