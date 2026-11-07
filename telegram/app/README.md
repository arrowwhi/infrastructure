# telegram/app

Каркас запуска сервиса (не только бота): конфиг из env/`.env` одним вызовом, логгер, трейсинг
и метрики (глобально — все коннекторы библиотеки подхватывают их сами), реестр health-проверок,
служебный порт с `/healthz`, `/ready`, `/metrics` и pprof, запуск долгоживущих компонентов и
остановка в правильном порядке по SIGINT/SIGTERM.

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import "github.com/arrowwhi/infrastructure/telegram/app"
```

## Пример

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/arrowwhi/infrastructure/telegram/app"
)

type Config struct {
	App      app.Config
	Postgres postgres.Config
}

func main() {
	ctx := context.Background()

	var cfg Config
	if err := app.LoadConfig(&cfg); err != nil {
		log.Fatal(err)
	}

	a, err := app.New(ctx, cfg.App) // логгер, трейсинг, метрики, health
	if err != nil {
		log.Fatal(err)
	}

	pool, err := postgres.New(ctx, cfg.Postgres)
	if err != nil {
		log.Fatal(err)
	}
	a.Health().Register("postgres", pool.Healthcheck)
	a.OnShutdown(func(context.Context) error { pool.Close(); return nil })

	worker := app.ComponentFunc(func(ctx context.Context) error {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				logger.Info(ctx).Msg("tick")
			}
		}
	})

	if err = a.Run(ctx, worker); err != nil { // блокируется до сигнала или ошибки компонента
		log.Fatal(err)
	}
}
```

```bash
SERVICE_NAME=my-service DB_USER=svc DB_DATABASE=app go run .
```

Компонент — всё, что реализует `Run(ctx context.Context) error` и возвращает `nil` при отмене `ctx`:
`*bot.Bot`, `*httpserver.Server`, `*grpcserver.Server` или функция через `app.ComponentFunc`.

## Конфигурация

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `SERVICE_NAME` | — | **обязательна**; имя в логах, метриках, трейсах |
| `SERVICE_VERSION`, `ENVIRONMENT` | `dev`, `local` | версия и окружение там же |
| `SHUTDOWN_TIMEOUT` | `30s` | сколько ждать остановки компонентов, `OnShutdown` и сброса телеметрии |
| `ADMIN_ENABLED` | `true` | служебный HTTP-порт |
| `ADMIN_HOST`, `ADMIN_PORT` | `0.0.0.0`, `9090` | его адрес |
| `ADMIN_PPROF_ENABLED` | `false` | `/debug/pprof/*` |
| `LOG_*` | см. [`observability/logger`](../../observability/logger/README.md) | `LOG_LEVEL=info`, `LOG_PRETTYOUTPUT=false`, `LOG_MASKING=true` |
| `METRICS_*` | см. [`observability/metrics`](../../observability/metrics/README.md) | `METRICS_EXPORTER=prometheus`, `METRICS_ENABLED=true`… |
| `OTEL_*` | см. [`observability/tracing`](../../observability/tracing/README.md) | `OTEL_ENABLED=false`, `OTEL_EXPORTER=otlp-grpc`, `OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317` |

`app.LoadConfig(&cfg)` заполняет любую структуру по тегам `env`, `env-default`, `env-prefix`
([cleanenv](https://github.com/ilyakaznacheev/cleanenv)) — поля `app.Config`, `postgres.Config`,
`redis.Config`, `bot.Config` можно класть в один конфиг сервиса рядом с собственными. Отсутствие
`env-required`-поля — ошибка.

## Служебный порт

| Путь | |
|---|---|
| `/healthz` | процесс жив; зависимости не проверяет — иначе падение базы перезапустит все поды |
| `/ready` | результат `a.Health()`, 503 при падении критичной проверки |
| `/metrics` | при `METRICS_EXPORTER=prometheus` |
| `/debug/pprof/*` | при `ADMIN_PPROF_ENABLED=true` |

Порт не публикуют наружу: на него смотрят Prometheus и пробы Kubernetes. `a.AdminHandler()` отдаёт
тот же обработчик для тестов или встраивания.

## Метрики, трейсы, логи

Сам пакет пишет `health.check.status`/`health.check.duration` (через `Health().RegisterMetrics`),
`app_build_info` (из `observability/metrics`) и логи старта/остановки. Всё остальное — компоненты.

## Порядок запуска и остановки

1. `app.New`: логгер → трейсинг → метрики → health-метрики.
2. `a.Run`: служебный сервер и все компоненты параллельно.
3. Остановка по SIGINT/SIGTERM, отмене `ctx` или первой ошибке любого компонента: компоненты
   получают отменённый `ctx` → функции `OnShutdown` в **обратном** порядке регистрации
   (созданное последним закрывается первым) → сброс метрик и трейсов.
   `Run` возвращает первую ошибку компонента вместе с ошибками остановки.

## Связь с другими пакетами

- [`telegram/bot`](../bot/README.md): `a.Run(ctx, b)` и `a.Health().Register("telegram", b.Healthcheck)`.
- [`http/server`](../../http/server/README.md), [`grpc/server`](../../grpc/server/README.md) — тоже компоненты;
  передавайте им `a.Health()` (`WithHealthRegistry`), чтобы проверки были одни на весь сервис.
- Коннекторы ([`connections/postgres`](../../connections/postgres/README.md),
  [`connections/redis`](../../connections/redis/README.md)) регистрируют проверку в `a.Health()` и закрытие в `a.OnShutdown`.

## Подводные камни

- `.env` из рабочего каталога читается первым и его значения **перекрывают** переменные окружения
  процесса (cleanenv делает `os.Setenv` для каждой строки). Не кладите `.env` в образ для прода.
- Пустая переменная (`ADMIN_PORT=`) в окружении или в `.env` — это значение, а не «не задано»:
  `env-default` не применится.
- `SHUTDOWN_TIMEOUT` читают и `app.Config`, и `tracing.Config` (у трейсинга нет префикса).
- `app.New` вызывает `logger.Init`, а он срабатывает один раз на процесс: второй `app.New` в тестах
  не поменяет уровень логов. Prometheus-экспортер тоже регистрируется глобально один раз — в тестах
  выставляйте `cfg.Metrics.Enabled = false`.
- Компонент, который возвращается сразу (не блокируется до отмены `ctx`) с `nil`, не останавливает
  приложение, но и не работает — `Run` должен блокироваться.
