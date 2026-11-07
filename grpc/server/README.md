# grpc/server

gRPC-сервер с инфраструктурной обвязкой: recovery паник, лог каждого RPC, OTel-метрики
(unary и stream), трейсинг через otelgrpc, health-сервис `grpc.health.v1` (в том числе на базе
общего `observability/health`), reflection, graceful shutdown с таймаутом и опциональный
HTTP/JSON-шлюз на [grpc-gateway](https://github.com/grpc-ecosystem/grpc-gateway).

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import grpcserver "github.com/arrowwhi/infrastructure/grpc/server"
```

## Пример

```go
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"

	grpcserver "github.com/arrowwhi/infrastructure/grpc/server"
	"github.com/arrowwhi/infrastructure/observability/health"
)

// usersService оборачивает сгенерированный код: pb.RegisterUsersServer и pb.RegisterUsersHandler.
type usersService struct{}

func (usersService) RegisterGRPC(s *grpc.Server) {
	// pb.RegisterUsersServer(s, &usersImpl{})
}

func (usersService) RegisterGateway(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
	// return pb.RegisterUsersHandler(ctx, mux, conn)
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	reg := health.New()
	reg.Register("db", func(context.Context) error { return nil })

	cfg := grpcserver.DefaultConfig()
	cfg.Gateway.Enabled = true // HTTP/JSON на :8081

	srv, err := grpcserver.New(cfg, "users-service",
		grpcserver.WithServices(usersService{}),
		grpcserver.WithHealthRegistry(reg, 10*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}

	if err = srv.Run(ctx); err != nil { // блокируется до отмены ctx
		log.Fatal(err)
	}
}
```

Сервис — любой тип с `RegisterGRPC(*grpc.Server)`; если он реализует ещё и `RegisterGateway`,
его методы публикуются через HTTP-шлюз. Регистрировать напрямую тоже можно: `pb.RegisterXxxServer(srv.GRPC(), impl)`.

## Конфигурация

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `GRPC_HOST`, `GRPC_PORT` | `0.0.0.0`, `50051` | адрес gRPC |
| `GRPC_MAX_RECV_MSG_SIZE`, `GRPC_MAX_SEND_MSG_SIZE` | `0`, `0` | лимит сообщения в байтах; `0` — по умолчанию gRPC (4 МБ на приём) |
| `GRPC_SHUTDOWN_TIMEOUT` | `30s` | ожидание активных RPC при остановке, затем принудительный `Stop` |
| `GRPC_REFLECTION_ENABLED` | `true` | server reflection (grpcurl, Postman) |
| `GRPC_HEALTH_ENABLED` | `true` | сервис `grpc.health.v1` |
| `GRPC_LOGGING_ENABLED` | `true` | лог каждого RPC |
| `GRPC_METRICS_ENABLED` | `true` | метрики `grpc.server.*` |
| `GRPC_TRACING_ENABLED` | `true` | спаны otelgrpc (и на шлюзе) |
| `GRPC_GATEWAY_ENABLED` | `false` | HTTP/JSON-шлюз |
| `GRPC_GATEWAY_HOST`, `GRPC_GATEWAY_PORT` | `0.0.0.0`, `8081` | адрес шлюза |
| `GRPC_GATEWAY_READ_HEADER_TIMEOUT` | `10s` | `ReadHeaderTimeout` HTTP-сервера шлюза |

Опции: `WithServices`, `WithHealthRegistry(reg, interval)`, `WithUnaryInterceptors`/`WithStreamInterceptors`
(выполняются после встроенных: recovery → logging → metrics → ваши), `WithServerOptions` (TLS-креды,
keepalive), `WithGatewayMuxOptions`, `WithGatewayMiddlewares` (CORS, авторизация), `WithListener` (тесты).

## Метрики, трейсы, логи

- Метрики (`observability/metrics`, метки `grpc.method`, `grpc.status`): `grpc.server.request.count`,
  `grpc.server.request.duration` (с), `grpc.server.request.size`, `grpc.server.response.size`,
  `grpc.server.active_requests`, `grpc.server.stream.messages`.
- Трейсы: otelgrpc на сервере; на шлюзе — спан otelhttp `<serviceName>.gateway`, связанный с gRPC-спаном.
  HTTP-метрик шлюз не пишет: каждый его запрос уже учтён как gRPC-вызов.
- Логи через `observability/logger` (с `trace_id`): одна запись `finished call` на RPC; уровень по коду
  (`OK`, `NotFound`, `InvalidArgument`… — info; `Unavailable`, `DeadlineExceeded`, `PermissionDenied`… — warn;
  `Internal`, `Unknown`, `Unimplemented` — error). Паника логируется и превращается в `Internal`.

## Связь с другими пакетами

- `observability/health`: `WithHealthRegistry(reg, interval)` опрашивает реестр каждые `interval`
  (≤ 0 — 10 с) и переводит весь сервер в `NOT_SERVING` при падении критичной проверки.
  Тот же реестр отдаёт `/ready` в `http/server` или `telegram` и метрики `health.check.*`.
- `telegram`: `*Server` реализует `telegram.Component` — `a.Run(ctx, srv)`; реестр — `a.Health()`.
- `grpc/client` — парный клиент; см. там про keepalive.

## Подводные камни

- `serviceName` обязателен (`New` вернёт ошибку при пустом).
- `Register` и `WithServices` — только до `Run`.
- При остановке health сначала переходит в `NOT_SERVING`, потом сервер ждёт активные RPC
  не дольше `GRPC_SHUTDOWN_TIMEOUT`.
- Шлюз подключается к собственному gRPC-порту **без TLS**. Если TLS включён через
  `WithServerOptions(grpc.Creds(...))`, шлюз не сможет подключиться.
- Keepalive-политика не задана: клиенты с частыми пингами (в том числе `grpc/client` по умолчанию)
  получат `GOAWAY too_many_pings` — задайте `grpc.KeepaliveEnforcementPolicy` через `WithServerOptions`.
- `WithHealthRegistry` требует `GRPC_HEALTH_ENABLED=true`.
