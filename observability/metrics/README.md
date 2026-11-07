# observability/metrics

Инициализация OpenTelemetry-метрик (Prometheus, OTLP gRPC, OTLP HTTP) и готовые
инструменты: HTTP-middleware, gRPC-интерсепторы для сервера и клиента, метрики рантайма Go
и `app_build_info`. `InitMeterProvider` регистрирует провайдер глобально
(`otel.SetMeterProvider`), поэтому все коннекторы библиотеки подхватывают его сами.
Все длительности — в секундах, гистограммы с границами `metrics.DurationBuckets`
(1 мс … 10 с).

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import "github.com/arrowwhi/infrastructure/observability/metrics"
```

## Пример

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/arrowwhi/infrastructure/observability/metrics"
)

func main() {
	ctx := context.Background()

	mp, err := metrics.InitMeterProvider(ctx, metrics.DefaultConfig(), "my-service", "1.0.0", "production")
	if err != nil {
		log.Fatal(err)
	}
	if mp != nil { // при Enabled=false провайдера нет
		defer func() { _ = mp.Shutdown(context.Background()) }()
	}

	r := chi.NewRouter()
	r.Use(metrics.Middleware("my-service")) // именно router.Use — нужен шаблон маршрута
	r.Get("/users/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	if h := metrics.Handler(); h != nil { // только для экспортёра prometheus
		r.Handle("/metrics", h)
	}

	log.Fatal(http.ListenAndServe(":8080", r))
}
```

gRPC без `grpc/server`:

```go
srv := grpc.NewServer(
	grpc.ChainUnaryInterceptor(metrics.UnaryServerInterceptor("my-service")),
	grpc.ChainStreamInterceptor(metrics.StreamServerInterceptor("my-service")),
)
```

Свои метрики — обычным OTel API: `otel.Meter("my-service").Int64Counter(...)`; для длительностей
используйте единицу `"s"` и `metric.WithExplicitBucketBoundaries(metrics.DurationBuckets...)`.

## Конфигурация

Префикс переменных задаёт тот, кто встраивает `Config`; в `telegram/app` это `METRICS_`.

| Переменная (`telegram/app`) | По умолчанию | Смысл |
|---|---|---|
| `METRICS_ENABLED` | `true` | `false` — провайдер не создаётся, `InitMeterProvider` возвращает `nil, nil` |
| `METRICS_EXPORTER` | `prometheus` | `prometheus`, `otlp-grpc`, `otlp-http` |
| `METRICS_OTLP_ENDPOINT` | `localhost:4317` | адрес коллектора для OTLP (`:4318` для otlp-http) |
| `METRICS_OTLP_INSECURE` | `true` | OTLP без TLS |
| `METRICS_METRIC_INTERVAL` | `60` | период отправки OTLP, секунды |
| `METRICS_RUNTIME_METRICS` | `true` | метрики рантайма Go для OTLP (для Prometheus их отдают стандартные `go_*`/`process_*`) |
| `METRICS_PROMETHEUS_ENDPOINT` | `:9090` | не используется: `/metrics` монтируется в ваш сервер через `metrics.Handler()` |

## Метрики

HTTP (`Middleware`), метки `http.method`, `http.route` (шаблон chi или `unmatched`), `http.status_code`:

| Метрика | Тип |
|---|---|
| `http.server.request.count` | счётчик |
| `http.server.request.duration` | гистограмма, с |
| `http.server.request.size`, `http.server.response.size` | гистограмма, байты |
| `http.server.active_requests` | up-down счётчик, только метка `http.method` |

gRPC, метки `grpc.method`, `grpc.status`:

| Метрика | |
|---|---|
| `grpc.server.request.count`, `grpc.server.request.duration` (с) | сервер |
| `grpc.server.request.size`, `grpc.server.response.size`, `grpc.server.active_requests`, `grpc.server.stream.messages` | сервер |
| `grpc.client.*` — те же имена | клиент (`UnaryClientInterceptor`/`StreamClientInterceptor`) |

Плюс `app.build.info` (в Prometheus — `app_build_info`) = 1 с метками `service`, `version`,
`environment`, `go_version`, `revision`. В Prometheus точки в именах становятся `_`, к счётчикам
добавляется `_total`, к единицам — суффикс (`http_server_request_duration_seconds`).

## Связь с другими пакетами

- `telegram/app` вызывает `InitMeterProvider` сам и отдаёт `/metrics` на служебном порту.
- `http/server`: middleware подключается опцией `httpserver.WithMiddlewares(metrics.Middleware(name))`;
  `/metrics` — на отдельном порту при `HTTP_METRICS_ENABLED=true`.
- `grpc/server` и `grpc/client` подключают интерсепторы сами (`GRPC_METRICS_ENABLED`, `EnableMetrics`).
- `connections/postgres`, `connections/redis`, `http/client`, `telegram/*`, `observability/health`
  пишут метрики в глобальный провайдер.

## Подводные камни

- `InitMeterProvider` при `Enabled=false` возвращает `nil`: `mp.Shutdown` без проверки на `nil` упадёт.
- Prometheus-экспортер регистрируется в глобальном `prometheus.DefaultRegisterer` один раз на
  процесс; повторный `InitMeterProvider` с `prometheus` (например, в тестах) вернёт ошибку
  дублирующейся регистрации.
- `Middleware`, обёрнутый вокруг роутера снаружи (а не `router.Use`), не видит шаблон
  маршрута: все запросы получат `http.route="unmatched"`.
