# grpc/client

Фабрика `*grpc.ClientConn` с готовой цепочкой интерсепторов: дедлайн по умолчанию, логирование
вызовов (zerolog, с `trace_id`), OTel-метрики `grpc.client.*`, повторы с backoff и джиттером,
спаны otelgrpc с пробросом trace context, keepalive и TLS/mTLS. Возвращается обычное
соединение — сгенерированные клиенты создаются из него как всегда.

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import grpcclient "github.com/arrowwhi/infrastructure/grpc/client"
```

## Пример

```go
package main

import (
	"context"
	"log"
	"time"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	grpcclient "github.com/arrowwhi/infrastructure/grpc/client"
	"github.com/arrowwhi/infrastructure/observability/logger"
)

func main() {
	cfg := &grpcclient.Config{
		Endpoints:     "users-service:50051",
		ServiceName:   "users-client",
		Timeout:       5 * time.Second, // дедлайн, если у вызова его нет
		EnableLogging: true,
		EnableMetrics: true,
		EnableTracing: true,
		EnableRetries: true,
		Retry: &grpcclient.RetryConfig{
			MaxRetries:      3,
			PerRetryTimeout: time.Second,
			BackoffTime:     100 * time.Millisecond,
			BackoffFraction: 0.3,
		},
		EnableKeepAlive: true,
		KeepAlive: &grpcclient.KeepAliveConfig{
			RefreshTime:         30 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: true,
		},
	}

	l := logger.With().Str("component", "grpc-client").Logger()

	conn, err := grpcclient.NewClient(cfg, &l)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// Любой сгенерированный клиент: pb.NewUsersClient(conn).
	resp, err := healthpb.NewHealthClient(conn).Check(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil {
		log.Fatal(err)
	}
	log.Println(resp.GetStatus())
}
```

Порядок unary-интерсепторов (снаружи внутрь): дедлайн по умолчанию → логирование → метрики →
повторы. Логи и метрики описывают логический вызов целиком (итоговый статус и полную длительность),
каждая попытка видна отдельным спаном и счётчиком повторов. Для стримов работают логирование,
метрики и трейсинг; дедлайн по умолчанию и повторы — только для unary.

## Конфигурация

У полей нет `env`-тегов: конфиг читается из YAML (ключи ниже) или собирается в коде.
`env-default` применяется только при загрузке через cleanenv.

| YAML-ключ | По умолчанию (`env-default`) | Смысл |
|---|---|---|
| `endpoints` | `localhost:50051` | цель `grpc.NewClient` (`host:port`, `dns:///...`) |
| `timeout` | `30s` | дедлайн вызова, если в `ctx` его нет; `0` — без дедлайна |
| `service_name` | `grpc-client` | имя meter'а |
| `enable_logging` | `true` | лог каждого вызова |
| `enable_metrics` | `true` | метрики `grpc.client.*` |
| `enable_tracing` | `true` | спаны otelgrpc + проброс trace context |
| `enable_retries` | `true` | повторы unary-вызовов |
| `retry.max_retries` | `3` | повторов после первой попытки (всего `MaxRetries+1`) |
| `retry.per_retry_timeout` | `0s` | дедлайн одной попытки; `0` — общий дедлайн вызова |
| `retry.backoff`, `retry.backoff_fraction` | `100ms`, `0.3` | линейный backoff и доля джиттера |
| `enable_keep_alive` | `true` | keepalive-пинги |
| `keep_alive.time`, `keep_alive.timeout`, `keep_alive.permit_without_stream` | `30s`, `5s`, `true` | параметры keepalive |
| `enable_tls` | `false` | TLS; без него соединение незашифровано |
| `tls_config.ca_file`, `tls_config.cert_file`, `tls_config.key_file` | — | CA сервера и клиентский сертификат для mTLS |

Повторяются коды `Unavailable`, `DeadlineExceeded`, `ResourceExhausted`, `Aborted`, `Internal`.

## Метрики, трейсы, логи

- Метрики (`observability/metrics`, метки `grpc.method`, `grpc.status`): `grpc.client.request.count`,
  `grpc.client.request.duration` (с), `grpc.client.request.size`, `grpc.client.response.size`,
  `grpc.client.active_requests`, `grpc.client.stream.messages`.
- `grpc.client.retry.count` с меткой `grpc.status` — число повторов.
- Трейсы: otelgrpc, спан на каждую попытку.
- Логи: переданный zerolog-логгер с навешанным `logger.TraceHook` — в записях есть `trace_id`/`span_id`.
  На вызов две записи, `started call` и `finished call`; успешные вызовы — на уровне `debug`,
  `Unavailable`/`Internal` — `warn`. Повтор — `warn` «gRPC retry attempt».

## Связь с другими пакетами

- Метрики и трейсы пишутся в глобальные провайдеры: инициализируйте `observability/metrics` и
  `observability/tracing` (или `telegram`) до создания клиента.
- Логгер берите из `observability/logger`: `l := logger.With().Logger()`; `nil` — без логов.
- Закрытие соединения при остановке: `a.OnShutdown(func(context.Context) error { return conn.Close() })`.

## Подводные камни

- Конфиг передаётся указателем; `Retry` и `KeepAlive` — тоже указатели. `EnableKeepAlive` с
  `KeepAlive: nil` молча отключает keepalive; `EnableRetries` с `Retry: nil` даёт одну попытку без повторов.
- `EnableTLS: true` с `TLSConfig: nil` приведёт к панике — задайте хотя бы `&grpcclient.TLSConfig{}`
  (системные корневые сертификаты).
- Повторяются любые методы, включая неидемпотентные; `Internal` и `Aborted` в списке кодов —
  для мутирующих RPC отключайте повторы (`EnableRetries: false`) или делайте методы идемпотентными.
- `grpc.NewClient` не подключается сразу: недоступный сервер обнаружится на первом вызове.
- Значения keepalive по умолчанию (пинг каждые 30 с, в том числе без активных стримов) строже, чем
  разрешает grpc-go-сервер по умолчанию (`MinTime` 5 мин, `PermitWithoutStream=false`): сервер
  ответит `GOAWAY too_many_pings` и разорвёт соединение. Для `grpc/server` добавьте
  `grpcserver.WithServerOptions(grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 20 * time.Second, PermitWithoutStream: true}))`
  или увеличьте `keep_alive.time`.
