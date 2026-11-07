# observability/health

Реестр проверок зависимостей (PostgreSQL, Redis, Telegram, любые функции). Проверки
регистрируются один раз, а их результат расходится по всем потребителям: HTTP-пробы
`/healthz` и `/ready`, gRPC-сервис `grpc.health.v1` и метрики `health.check.*`.
Каждая проверка выполняется со своим таймаутом, все проверки — параллельно;
некритичные проверки не снимают сервис с трафика.

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import "github.com/arrowwhi/infrastructure/observability/health"
```

## Пример

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/arrowwhi/infrastructure/connections/redis"
	"github.com/arrowwhi/infrastructure/observability/health"
	"go.opentelemetry.io/otel"
)

func main() {
	ctx := context.Background()

	pool, err := postgres.New(ctx, postgres.DefaultConfig())
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	rdb, err := redis.New(ctx, redis.Config{Addr: "localhost:6379"})
	if err != nil {
		log.Fatal(err)
	}
	defer rdb.Close()

	reg := health.New()
	reg.Register("postgres", pool.Healthcheck)
	reg.Register("redis", rdb.Healthcheck, health.NonCritical()) // кэш: без него медленнее, но работаем

	if err = reg.RegisterMetrics(otel.Meter("my-service")); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health.LivenessHandler()) // процесс жив
	mux.Handle("GET /ready", reg.ReadinessHandler())     // 200 ok/degraded, 503 fail

	log.Fatal(http.ListenAndServe(":9090", mux))
}
```

Ответ `/ready`:

```json
{"checks":{"postgres":{"status":"ok","duration_ns":1200000,"critical":true}},"checked_at":"...","status":"ok"}
```

## Конфигурация

Переменных окружения нет — всё задаётся в коде:

| Опция | По умолчанию | Смысл |
|---|---|---|
| `health.WithTimeout(d)` (реестр) | `health.DefaultTimeout` = 2s | таймаут проверки по умолчанию |
| `health.WithCheckTimeout(d)` (проверка) | таймаут реестра | таймаут конкретной проверки |
| `health.NonCritical()` (проверка) | критичная | падение даёт `degraded`, а не `fail` |

Итоговый статус: `ok` — всё прошло; `degraded` — упали только некритичные (200, трафик идёт);
`fail` — упала критичная (503).

## Метрики, трейсы, логи

`Registry.RegisterMetrics(meter)` публикует результат **последнего** `Check`:

| Метрика | Метки | |
|---|---|---|
| `health.check.status` | `check`, `critical` | 1 — ok, 0 — fail |
| `health.check.duration` | `check`, `critical` | длительность проверки, с (gauge) |

Трейсов и логов пакет не пишет.

## Связь с другими пакетами

- `telegram/app` создаёт реестр сам: `a.Health().Register(...)`, отдаёт `/healthz` и `/ready`
  на служебном порту и регистрирует метрики.
- `http/server` отдаёт `/healthz` и `/ready` на основном порту; общий реестр передаётся
  через `httpserver.WithHealthRegistry(reg)`, проверки добавляются `srv.AddHealthCheckFunc(...)`
  или `srv.Health().Register(...)`.
- `grpc/server` с `grpcserver.WithHealthRegistry(reg, interval)` опрашивает реестр и переводит
  `grpc.health.v1` в `NOT_SERVING` при падении критичной проверки.
- Готовые проверки: `postgres.Pool.Healthcheck`, `redis.Client.Healthcheck`, `bot.Bot.Healthcheck`,
  `api.Client.Healthcheck` — все имеют сигнатуру `health.CheckFunc`.

## Подводные камни

- Метрики отражают `Registry.Last()`: если никто не вызывает `Check` (нет проб Kubernetes,
  gRPC-сервера с `WithHealthRegistry` или `Registry.Watch`), значения не обновляются.
  Для фонового опроса: `go reg.Watch(ctx, 15*time.Second, func(health.Report) {})`.
- `LivenessHandler` намеренно не проверяет зависимости: падение базы должно снимать под
  с трафика (`/ready`), а не перезапускать все поды.
- Проверка обязана уважать отмену `ctx`, иначе таймаут не поможет и горутина повиснет.
- Повторная регистрация с тем же именем заменяет прежнюю проверку.
