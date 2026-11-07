# Tracing

OpenTelemetry трассировка для Go приложений.

## Возможности

- 🔍 Автоматическая трассировка HTTP запросов
- 📊 Экспорт в Jaeger, Tempo, и другие OTLP-совместимые бэкенды
- 🎯 Настраиваемый sampling
- 📝 Stdout экспорт для разработки
- 🔗 W3C Trace Context propagation
- 🏷️ Семантические конвенции OpenTelemetry

## Быстрый старт

### 1. Инициализация

```go
import "github.com/arrowwhi/infrastructure/observability/tracing"

cfg := tracing.Config{
    Enabled:      true,        // по умолчанию false
    Exporter:     "otlp-grpc", // или "otlp-http", "stdout" (только для отладки)
    SamplingRate: 1.0,
}

tp, err := tracing.InitTracer(context.Background(), cfg, "my-service", "1.0.0", "development")
if err != nil {
    log.Fatal(err)
}
defer tp.Shutdown(context.Background())
```

### 2. HTTP Middleware

```go
import (
    httpserver "github.com/arrowwhi/infrastructure/http/server"
    "github.com/arrowwhi/infrastructure/observability/tracing"
)

srv := httpserver.New(
    cfg,
    log,
    tracing.Middleware("my-service"),
)
```

### 3. Ручные spans

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
)

tracer := otel.Tracer("my-service")

ctx, span := tracer.Start(r.Context(), "getUserFromDB",
    trace.WithAttributes(
        attribute.String("user.id", userID),
        attribute.String("db.system", "postgresql"),
    ),
)
defer span.End()

// Выполняем операцию
user, err := db.GetUser(ctx, userID)
if err != nil {
    span.RecordError(err)
    span.SetStatus(codes.Error, "failed to get user")
    return err
}

span.SetAttributes(
    attribute.String("user.name", user.Name),
)
span.SetStatus(codes.Ok, "user retrieved")
```

## Конфигурация

### Переменные окружения

```bash
OTEL_SERVICE_NAME=my-service
OTEL_SERVICE_VERSION=1.0.0
OTEL_ENVIRONMENT=production
OTEL_ENABLED=true
OTEL_EXPORTER=otlp-grpc
OTEL_EXPORTER_OTLP_ENDPOINT=jaeger:4317
OTEL_EXPORTER_OTLP_INSECURE=false
OTEL_SAMPLING_RATE=0.1
```

### Экспортеры

#### Stdout (для разработки)

```go
cfg := tracing.Config{
    Exporter: "stdout",
}
```

Выводит spans в stdout в JSON формате.

#### OTLP gRPC (для Jaeger/Tempo)

```go
cfg := tracing.Config{
    Exporter:     "otlp-grpc",
    OTLPEndpoint: "jaeger:4317",
    OTLPInsecure: false,
}
```

## Sampling

Используется `ParentBased(TraceIDRatioBased(SamplingRate))`: если у входящего запроса
есть родительский спан, его решение о сэмплировании сохраняется, а `SamplingRate`
применяется только к корневым спанам. Так трейс не рвётся между сервисами.

Sampling позволяет трассировать только часть запросов для снижения нагрузки:

```go
cfg := tracing.Config{
    SamplingRate: 0.1, // 10% запросов
}
```

- `1.0` - трассировать все запросы (для dev)
- `0.1` - трассировать 10% запросов (для prod)
- `0.01` - трассировать 1% запросов (для highload)

## Лучшие практики

### 1. Создавайте spans для важных операций

```go
// ✅ Good
ctx, span := tracer.Start(ctx, "fetchUserData")
defer span.End()

user, err := db.GetUser(ctx, id)
```

### 2. Добавляйте полезные атрибуты

```go
// ✅ Good
span.SetAttributes(
    attribute.String("user.id", userID),
    attribute.Int("result.count", len(results)),
    attribute.String("cache.status", "hit"),
)
```

### 3. Записывайте ошибки

```go
// ✅ Good
if err != nil {
    span.RecordError(err)
    span.SetStatus(codes.Error, "operation failed")
    return err
}
```

### 4. Используйте события для важных моментов

```go
span.AddEvent("cache miss, fetching from DB")
span.AddEvent("validation completed")
```

### 5. Передавайте контекст через границы

```go
// ✅ Good - контекст передается через HTTP headers
req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
resp, err := client.Do(req)
```

## Jaeger UI

После запуска Jaeger:

1. Открыть http://localhost:16686
2. Выбрать сервис в списке
3. Нажать "Find Traces"
4. Кликнуть на trace для просмотра деталей

### Что смотреть в Jaeger:

- **Waterfall** - визуализация вложенных операций
- **Duration** - время выполнения каждого span
- **Tags** - атрибуты spans
- **Logs** - события и ошибки
- **Process** - метаданные сервиса

## Примеры

См. `examples/http/main.go` для полного примера с:
- Простыми spans
- Вложенными spans
- Параллельными операциями
- Обработкой ошибок
- Различными атрибутами

## Интеграции

### PostgreSQL

```go
ctx, span := tracer.Start(ctx, "db.query",
    trace.WithAttributes(
        semconv.DBSystemPostgreSQL,
        semconv.DBStatementKey.String(query),
    ),
)
defer span.End()

rows, err := db.QueryContext(ctx, query)
```

### Redis

```go
ctx, span := tracer.Start(ctx, "redis.get",
    trace.WithAttributes(
        semconv.DBSystemRedis,
        attribute.String("redis.key", key),
    ),
)
defer span.End()

val, err := rdb.Get(ctx, key).Result()
```

### gRPC

```go
// Автоматически с grpc-ecosystem/go-grpc-middleware/v2
```

## Troubleshooting

### Spans не появляются в Jaeger

1. Проверьте, что Jaeger запущен: `docker ps | grep jaeger`
2. Проверьте endpoint: `OTEL_EXPORTER_OTLP_ENDPOINT`
3. Проверьте, что `Enabled: true`
4. Проверьте sampling rate

### Слишком много spans

Уменьшите sampling rate:

```go
cfg.SamplingRate = 0.01 // 1%
```

### Ошибки сертификатов

Для локальной разработки:

```go
cfg.OTLPInsecure = true
```

