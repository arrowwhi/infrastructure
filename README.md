# Infrastructure

Библиотека общей инфраструктуры для Go проектов.

## Компоненты

### HTTP Server (`http/server`)

Chi-based HTTP сервер с поддержкой:
- Graceful shutdown
- Middleware (RequestID, RealIP, Recoverer)
- Трассировка OpenTelemetry
- Настраиваемые таймауты

### Observability

#### Logger (`observability/logger`)
Zerolog-based логгер с маскировкой чувствительных данных.

#### Tracing (`observability/tracing`)
OpenTelemetry трассировка с поддержкой:
- OTLP gRPC экспорт (Jaeger, Tempo и т.д.)
- Stdout экспорт для разработки
- Автоматическая трассировка HTTP запросов
- Настраиваемый sampling

### Database

#### PostgreSQL (`connections/postgres`)

Коннектор поверх pgx v5 — см. [`connections/postgres/README.md`](connections/postgres/README.md):
- трейсинг и метрики OpenTelemetry, **выключены по умолчанию** (при выключенной
  инструментации трейсер не создаётся, накладных расходов нет)
- транзакции через контекст: `InTx`, вложенность через `SAVEPOINT`, перезапуск при
  конфликте сериализации
- повторы только для запросов, гарантированно не дошедших до сервера
- серверные таймауты, multi-host failover, mTLS, ротация секретов
- `CopyFrom`, batch, генерик-хелперы `Get[T]`/`Select[T]`, query builder
- advisory locks, LISTEN/NOTIFY, разделение чтения и записи, readiness-проба
- миграции — опциональный подпакет `connections/postgres/migrate`

- Redis (`connections/redis`) — не реализовано
- Transaction Manager (`utils/txmanager`) — координация транзакции PostgreSQL
  и конвейера Redis; для транзакций только по PostgreSQL используйте `postgres.Pool.InTx`

### gRPC (`grpc/client`, `grpc/server`)

- **Клиент** (`grpc/client`): TLS/mTLS, ретраи с backoff, keepalive, логирующий interceptor.
- **Сервер** (`grpc/server`): recovery, логирование RPC, OTel-метрики (unary и stream),
  трейсинг через otelgrpc, health-сервис `grpc.health.v1`, reflection, graceful shutdown с таймаутом
  и опциональный HTTP/JSON-шлюз (grpc-gateway) с метриками и трейсами.

```go
srv, err := grpcserver.New(cfg.GRPC, cfg.ServiceName,
    grpcserver.WithServices(usersService), // RegisterGRPC + (опционально) RegisterGateway
)
if err != nil { ... }
if err := srv.Run(ctx); err != nil { ... } // блокируется до отмены ctx
```

## Примеры

### HTTP Server с observability

```bash
# Запустить observability стек
make obs-up

# Запустить пример HTTP сервера
cd examples/http
go run .

# Открыть UI
open http://localhost:16686  # Jaeger (трейсинг)
open http://localhost:9090   # Prometheus (метрики)
open http://localhost:3000   # Grafana (визуализация)

# Сделать запросы
curl http://localhost:8080/health
curl http://localhost:8080/api/users/123
curl -X POST http://localhost:8080/api/orders
curl http://localhost:8080/api/parallel
```

### Конфигурация трассировки

```go
tracingCfg := tracing.Config{
    Enabled:      true,
    Exporter:     "otlp-grpc", // или "stdout"
    OTLPEndpoint: "jaeger:4317",
    OTLPInsecure: false,
    SamplingRate: 0.1, // 10% запросов
}

tp, err := tracing.InitTracer(context.Background(), tracingCfg, "my-service", "1.0.0", "production")
defer tp.Shutdown(context.Background())

// Создать сервер с трассировкой
srv := httpserver.New(
    cfg,
    log,
    tracing.Middleware("my-service"),
)
```

### Ручное создание spans

```go
tracer := otel.Tracer("my-service")

ctx, span := tracer.Start(r.Context(), "operationName",
    trace.WithAttributes(
        attribute.String("key", "value"),
    ),
)
defer span.End()

// Добавить события
span.AddEvent("something happened")

// Записать ошибку
if err != nil {
    span.RecordError(err)
    span.SetStatus(codes.Error, "operation failed")
}
```

## Зависимости

```bash
go get github.com/arrowwhi/infrastructure
```
