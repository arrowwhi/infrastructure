# http/server

HTTP-сервер на [chi](https://github.com/go-chi/chi) с graceful shutdown, TLS/mTLS,
опциональным HTTP/3, пробами `/healthz` и `/ready` на базе `observability/health`,
Swagger UI и отдельным служебным портом для `/metrics` и pprof. Встроенные middleware:
recovery, `X-Request-ID`, real IP и access-лог. Трейсинг и метрики подключаются опциями.

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import httpserver "github.com/arrowwhi/infrastructure/http/server"
```

## Пример

```go
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	httpserver "github.com/arrowwhi/infrastructure/http/server"
	"github.com/arrowwhi/infrastructure/observability/metrics"
	"github.com/arrowwhi/infrastructure/observability/tracing"
)

func main() {
	ctx := context.Background()

	cfg := httpserver.Config{
		Host:              "0.0.0.0",
		Port:              "8080",
		MetricsPort:       "9090",
		MetricsEnabled:    true, // /metrics на :9090
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		ShutdownTimeout:   30 * time.Second,
	}

	srv, err := httpserver.New(cfg,
		httpserver.WithMiddlewares(
			tracing.Middleware("my-service"),
			metrics.Middleware("my-service"),
		),
	)
	if err != nil {
		log.Fatal(err)
	}

	srv.AddHealthCheckFunc("db", func(ctx context.Context) error { return nil }) // для /ready

	srv.Router().Get("/hello/{name}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})

	if err = srv.Run(ctx); err != nil { // блокируется до SIGINT/SIGTERM или отмены ctx
		log.Fatal(err)
	}
}
```

Трейсинг и метрики предварительно инициализируются (`tracing.InitTracer`, `metrics.InitMeterProvider`)
или через `telegram`.

## Конфигурация

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `HTTP_HOST`, `HTTP_PORT` | `0.0.0.0`, `8080` | адрес основного сервера |
| `HTTP_METRICS_ENABLED` | `false` | служебный сервер с `/metrics` на `HTTP_METRICS_PORT` |
| `HTTP_METRICS_PORT` | `9090` | порт служебного сервера (хост — `HTTP_HOST`) |
| `HTTP_PPROF_ENABLED` | `false` | `/debug/pprof/*` на служебном порту |
| `HTTP_HTTP3_ENABLED` | `false` | HTTP/3 (QUIC) вместо HTTP/1.1+2; требует TLS |
| `HTTP_SWAGGER_ENABLED` | `false` | Swagger UI |
| `DOCS_PATH`, `DOCS_SWAGGER_URL`, `DOCS_SPEC_FILE` | `/docs`, `/openapi.yaml`, `openapi.yaml` | где UI, по какому URL и из какого файла отдаётся спецификация |
| `HTTP_TLS_ENABLED`, `HTTP_MTLS_ENABLED` | `false`, `false` | TLS и проверка клиентских сертификатов |
| `HTTP_TLS_CERT_FILE`, `HTTP_TLS_KEY_FILE`, `HTTP_TLS_CA_FILE` | — | сертификаты файлами |
| `TLS_CERT_DATA`, `TLS_KEY_DATA`, `TLS_CA_DATA` | — | сертификаты содержимым (приоритетнее файлов) |
| — (`ReadTimeout`, `WriteTimeout`, `IdleTimeout`, `ReadHeaderTimeout`, `ShutdownTimeout`) | `30s`, `30s`, `120s`, `10s`, `30s` | только yaml / код, переменных окружения нет |
| — (`TLS.Base64Encoded`) | `false` | `*_DATA` в base64; только yaml / код |

## Эндпоинты и логи

| Путь | Порт | |
|---|---|---|
| `/healthz` | основной | liveness, всегда 200 |
| `/ready` | основной | результат `Health()`-реестра, 503 при падении критичной проверки |
| `/docs/*`, `/openapi.yaml` | основной | при `HTTP_SWAGGER_ENABLED` |
| `/metrics`, `/debug/pprof/*` | служебный | при `HTTP_METRICS_ENABLED` / `HTTP_PPROF_ENABLED` |

Встроенные middleware (в этом порядке): `Recoverer` → `middlewares.RequestID` → `middlewares.RealIP`
→ `middlewares.Log`, затем ваши из `WithMiddleware(s)`. Access-лог — одна запись
`incoming HTTP request` на запрос (уровень по статусу: info / warn для 4xx / error для 5xx) с
методом, путём, статусом, `latency`, `request_id` и телами запроса/ответа (до 512 КБ).
Подробнее — [`middlewares`](middlewares/README.md).

Метрики (`http.server.*`) и трейсы появляются только при подключении `metrics.Middleware` и
`tracing.Middleware` — см. [`observability/metrics`](../../observability/metrics/README.md) и
[`observability/tracing`](../../observability/tracing/README.md).

## Связь с другими пакетами

- Health: общий реестр — `httpserver.WithHealthRegistry(reg)`; проверки — `srv.AddHealthCheckFunc(name, fn, opts...)`
  или `srv.Health().Register(...)`. Тот же реестр можно отдать в `grpc/server.WithHealthRegistry`.
  `AddHealthCheck(HealthChecker)` устарел.
- `telegram`: `*Server` реализует `telegram.Component` (`Run(ctx) error`) — передайте его в `a.Run(ctx, srv)`.
  Тогда реестр лучше брать из приложения: `httpserver.WithHealthRegistry(a.Health())`, а `/metrics`
  уже есть на служебном порту приложения — `HTTP_METRICS_ENABLED` не включайте.
- `WithShutdownHook(fn)` — функции, вызываемые после остановки сервера (в порядке регистрации).

## Подводные камни

- Таймауты и `ShutdownTimeout` не читаются из env: при сборке `Config` руками нулевые значения
  означают «без таймаута» (кроме `ShutdownTimeout`: 0 — остановка без ожидания).
- `Router()` возвращает `Group`: `Use` на нём действует только на маршруты, зарегистрированные
  через этот же объект. Глобальные middleware — только через `WithMiddleware(s)`.
- `middlewares.Log` стоит раньше пользовательских middleware, поэтому в access-логе нет `trace_id`
  (спан создаётся позже) и тела запросов/ответов логируются целиком до 512 КБ — не возвращайте
  в ответах секреты и учитывайте объём логов.
- Если обработчик не вызвал `WriteHeader`, access-лог пишет `status_code: 0`.
- `Run` сам ловит SIGINT/SIGTERM. Под `telegram` это безопасно, но остановка может начаться
  с сервера раньше, чем приложение отменит контекст.
- HTTP/3 не поддерживает `ReadTimeout`/`WriteTimeout` и останавливается без ожидания активных запросов.
