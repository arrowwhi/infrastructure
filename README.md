# Infrastructure

Библиотека общей инфраструктуры для Go-сервисов: коннекторы к PostgreSQL и Redis, HTTP- и
gRPC-серверы и клиенты, логи, метрики, трейсы и health-проверки на OpenTelemetry, каркас
запуска сервиса и Telegram-бота. Все пакеты одинаково настраиваются из переменных окружения,
пишут телеметрию в глобальные провайдеры OpenTelemetry и подключаются к общему реестру
health-проверок.

## Пакеты

| Пакет | Назначение | Документация |
|---|---|---|
| `connections/postgres` | Пул pgx v5: транзакции через контекст, безопасные повторы, реплики, `Healthcheck`, OTel | [README](connections/postgres/README.md) |
| `connections/postgres/migrate` | Миграции goose поверх `postgres.Pool` под advisory-блокировкой | [README](connections/postgres/migrate/README.md) |
| `connections/redis` | Клиент go-redis v9 с redisotel, проверкой при старте и `Healthcheck` | [README](connections/redis/README.md) |
| `grpc/server` | gRPC-сервер: recovery, логи, метрики, трейсы, `grpc.health.v1`, reflection, grpc-gateway | [README](grpc/server/README.md) |
| `grpc/client` | `*grpc.ClientConn` с дедлайном по умолчанию, повторами, логами, метриками, трейсами, TLS | [README](grpc/client/README.md) |
| `http/server` | HTTP-сервер на chi: graceful shutdown, TLS/HTTP3, `/healthz` `/ready`, Swagger, служебный порт | [README](http/server/README.md) |
| `http/server/middlewares` | Request ID, real IP, access-лог | [README](http/server/middlewares/README.md) |
| `http/client` | Fluent HTTP-клиент: повторы, (де)сериализация, `HTTPError`, mTLS, OTel | [README](http/client/README.md) |
| `observability/logger` | Глобальный zerolog-логгер с `trace_id` и маскированием | [README](observability/logger/README.md) |
| `observability/metrics` | OTel-метрики (Prometheus/OTLP), HTTP-middleware, gRPC-интерсепторы, `app_build_info` | [README](observability/metrics/README.md) |
| `observability/tracing` | OTel-трейсинг (OTLP gRPC/HTTP, stdout), `ParentBased`-сэмплинг, HTTP-middleware | [README](observability/tracing/README.md) |
| `observability/health` | Реестр проверок для `/ready`, gRPC health и метрик `health.check.*` | [README](observability/health/README.md) |
| `telegram` | Telegram-бот целиком: запуск сервиса (конфиг, логи, метрики, трейсы, служебный порт, порядок остановки), маршруты и шаги диалога, клиент Bot API с лимитами и повтором после 429, хранилище шагов (память, PostgreSQL, Redis). Старые `telegram/app`, `telegram/bot`, `telegram/api`, `telegram/state` оставлены как алиасы | [README](telegram/README.md) |
| `utils/masking` | Маскирование персональных данных в JSON, XML и `map[string]any` | [README](utils/masking/README.md) |
| `utils/txmanager` | Общая транзакция PostgreSQL + конвейер Redis | [README](utils/txmanager/README.md) |

Обзор: [`observability`](observability/README.md) — локальный стенд OTel Collector + Jaeger + Prometheus + Grafana.

## Быстрый старт

```bash
go get github.com/arrowwhi/infrastructure@latest
```

Типичный `main`: конфиг из env, инфраструктура из `telegram`, коннекторы с health-проверками
и закрытием при остановке, компоненты (бот, HTTP-/gRPC-сервер) под общим `Run`.

```go
package main

import (
	"context"
	"log"

	"github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/arrowwhi/infrastructure/connections/redis"
	"github.com/arrowwhi/infrastructure/observability/health"
	"github.com/arrowwhi/infrastructure/telegram"
)

// Config — вся конфигурация сервиса, читается из env (и .env) одним вызовом.
type Config struct {
	App      telegram.AppConfig
	Postgres postgres.Config
	Redis    redis.Config
	Telegram telegram.Config
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()

	var cfg Config
	if err := telegram.LoadConfig(&cfg); err != nil {
		return err
	}

	// Логгер, трейсинг, метрики, health-реестр и служебный порт :9090 (/healthz, /ready, /metrics).
	a, err := telegram.NewApp(ctx, cfg.App)
	if err != nil {
		return err
	}

	pool, err := postgres.New(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	a.Health().Register("postgres", pool.Healthcheck)
	a.OnShutdown(func(context.Context) error { pool.Close(); return nil })

	rdb, err := redis.New(ctx, cfg.Redis)
	if err != nil {
		return err
	}
	a.Health().Register("redis", rdb.Healthcheck, health.NonCritical())
	a.OnShutdown(func(context.Context) error { return rdb.Close() })

	b, err := telegram.New(cfg.Telegram, telegram.WithStateStore(telegram.NewRedisStore(rdb.Client, "")))
	if err != nil {
		return err
	}
	a.Health().Register("telegram", b.Healthcheck)

	b.Command("start", "Начать", func(c *telegram.Context) error {
		_, err := c.Reply("Привет!")
		return err
	})

	// Блокируется до SIGINT/SIGTERM; затем: компоненты → OnShutdown (LIFO) → сброс телеметрии.
	return a.Run(ctx, b)
}
```

```bash
SERVICE_NAME=my-bot TELEGRAM_BOT_TOKEN=123:abc \
DB_USER=svc DB_PASSWORD=secret DB_DATABASE=app \
REDIS_ADDR=localhost:6379 go run .
```

HTTP- и gRPC-серверы подключаются так же — это компоненты с `Run(ctx) error`:
`a.Run(ctx, b, httpSrv, grpcSrv)`, а общий реестр проверок передаётся им через
`httpserver.WithHealthRegistry(a.Health())` и `grpcserver.WithHealthRegistry(a.Health(), interval)`.

## Как пакеты работают вместе

- **Конфиг.** У каждого пакета `Config` с тегами `env`/`env-default`; `telegram.LoadConfig` читает их
  все одной структурой. Таблицы переменных — в README пакетов.
- **Телеметрия.** `telegram` (или `tracing.InitTracer` + `metrics.InitMeterProvider`) регистрирует
  провайдеры глобально; коннекторы, серверы и клиенты пишут в них без дополнительной настройки.
  Трейсинг по умолчанию выключен (`OTEL_ENABLED=false`), длительности во всех метриках библиотеки —
  в секундах (кроме метрик пула redisotel).
- **Логи.** `observability/logger` добавляет `trace_id`/`span_id` в каждую запись с контекстом спана
  и маскирует персональные данные.
- **Health.** Один `health.Registry` отдаёт `/ready` (`telegram` или `http/server`), переключает
  `grpc.health.v1` (`grpc/server`) и публикует `health.check.*`. Готовые проверки: `postgres.Pool.Healthcheck`,
  `redis.Client.Healthcheck`, `telegram.Bot.Healthcheck`.
- **Остановка.** `telegram.App.OnShutdown` закрывает ресурсы в обратном порядке после остановки компонентов.

## Примеры

| Каталог | Что показывает |
|---|---|
| [`examples/tgbot`](examples/tgbot/main.go) | бот на пакете `telegram` с диалогом и выбором хранилища состояний |
| [`examples/http`](examples/http/main.go) | HTTP-сервер с трейсингом, метриками и PostgreSQL |
| [`examples/docker-compose.yml`](examples/docker-compose.yml) | стенд OTel Collector, Jaeger, Prometheus, Grafana — см. [`observability`](observability/README.md) |

```bash
make obs-up       # стенд наблюдаемости
make run-server   # examples/http на :8080
make load-test    # нагрузка, чтобы графики ожили
```

Jaeger — http://localhost:16686, Prometheus — http://localhost:9090, Grafana — http://localhost:3000 (admin/admin).
