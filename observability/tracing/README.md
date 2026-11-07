# observability/tracing

Инициализация OpenTelemetry-трейсинга: экспорт в OTLP gRPC / OTLP HTTP (Jaeger, Tempo,
OTel Collector) или в stdout для отладки, сэмплер `ParentBased(TraceIDRatioBased)`,
пропагация W3C Trace Context + Baggage и HTTP-middleware для chi. `InitTracer` регистрирует
провайдер глобально (`otel.SetTracerProvider`), поэтому все коннекторы библиотеки
подхватывают его сами. **По умолчанию трейсинг выключен** (`OTEL_ENABLED=false`): без
коллектора экспортёр только шумит ошибками в логах.

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import "github.com/arrowwhi/infrastructure/observability/tracing"
```

## Пример

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/arrowwhi/infrastructure/observability/tracing"
)

func main() {
	ctx := context.Background()

	cfg := tracing.DefaultConfig()
	cfg.Enabled = true
	cfg.OTLPEndpoint = "localhost:4317"
	cfg.SamplingRate = 0.1 // 10% корневых трейсов

	tp, err := tracing.InitTracer(ctx, cfg, "my-service", "1.0.0", "production")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	r := chi.NewRouter()
	r.Use(tracing.Middleware("my-service"))
	r.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := otel.Tracer("my-service").Start(r.Context(), "loadUser")
		defer span.End()

		span.SetAttributes(attribute.String("user.id", "42"))
		if err := loadUser(ctx); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "load user failed")
			http.Error(w, "internal error", http.StatusInternalServerError)

			return
		}

		w.WriteHeader(http.StatusOK)
	})

	log.Fatal(http.ListenAndServe(":8080", r))
}

func loadUser(context.Context) error { return nil }
```

## Конфигурация

Теги — полные имена переменных (без префикса).

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `OTEL_ENABLED` | `false` | включить трейсинг; при `false` ставится noop-провайдер |
| `OTEL_EXPORTER` | `otlp-grpc` | `otlp-grpc`, `otlp-http`, `stdout` (только отладка: печатает каждый спан) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | `host:port` коллектора (для otlp-http обычно `:4318`) |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` | без TLS |
| `OTEL_SAMPLING_RATE` | `1.0` | доля **корневых** трейсов, 0.0–1.0 |
| `SHUTDOWN_TIMEOUT` | `5s` | не используется кодом; таймаут остановки задаёт контекст `tp.Shutdown(ctx)` |

Сэмплинг: если у входящего запроса уже есть родительский спан (`traceparent`), его решение
сохраняется — трейс не рвётся между сервисами. `SamplingRate` применяется только к корням:
`1.0` — dev, `0.1`–`0.01` — прод.

## Трейсы

- `tracing.Middleware` (otelhttp): спан на каждый HTTP-запрос, имя `METHOD /route/{pattern}`,
  атрибуты `http.route` и `http.query` (сырой query string).
- Спаны коннекторов: `connections/postgres` (при `DB_TRACING_ENABLED`), `connections/redis`,
  `http/client`, `grpc/client`, `grpc/server`, `telegram/*` — все пишут в глобальный провайдер.
- Ресурс: `service.name`, `service.version`, `deployment.environment`.

## Связь с другими пакетами

- `telegram/app` вызывает `InitTracer` сам из `app.Config.Tracing` и делает `Shutdown` в конце.
- `http/server`: `httpserver.WithMiddlewares(tracing.Middleware(name))`.
- `observability/logger` добавляет `trace_id`/`span_id` в каждую запись с контекстом спана.

## Подводные камни

- Контекст из `Start` нужно переприсваивать: `ctx, span := tracer.Start(ctx, ...)`. С `_, span :=`
  дочерние спаны (запросы в базу) окажутся рядом, а не внутри.
- `tp.Shutdown` сбрасывает буфер спанов; без него последние спаны при остановке теряются.
- `http.query` пишется в спан как есть: не передавайте персональные данные и токены в query string.
- Атрибут `http.route` ставится до маршрутизации: при подключении через `router.Use` на корневом
  роутере (так делает `httpserver.WithMiddlewares`) шаблон ещё неизвестен и атрибута не будет;
  имя спана при этом корректное (`GET /users/{id}`). Атрибут появится, если подключить middleware
  через `r.Group(func(r chi.Router) { r.Use(tracing.Middleware(name)); ... })` или `r.With(...)`.
- `SHUTDOWN_TIMEOUT` в `telegram/app` читается и `app.Config`, и `tracing.Config` (у трейсинга нет
  префикса): одна переменная задаёт оба значения.
