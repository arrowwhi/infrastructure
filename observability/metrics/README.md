# Metrics Package

Пакет для сбора и экспорта метрик приложения с использованием OpenTelemetry.

## Возможности

- Поддержка нескольких экспортеров: Prometheus, OTLP gRPC, OTLP HTTP
- HTTP middleware для автоматического сбора метрик запросов
- gRPC interceptors для автоматического сбора метрик gRPC запросов
- Гибкая конфигурация
- Интеграция с OpenTelemetry

## Собираемые HTTP метрики

- `http.server.request.count` - общее количество HTTP запросов
- `http.server.request.duration` - длительность обработки запросов (ms)
- `http.server.request.size` - размер запросов (bytes)
- `http.server.response.size` - размер ответов (bytes)
- `http.server.active_requests` - количество активных запросов

Все метрики включают атрибуты:
- `http.method` - HTTP метод
- `http.route` - путь запроса
- `http.status_code` - код ответа

## Собираемые gRPC метрики

- `grpc.server.request.count` - общее количество gRPC запросов
- `grpc.server.request.duration` - длительность обработки запросов (ms)
- `grpc.server.request.size` - размер запросов (bytes)
- `grpc.server.response.size` - размер ответов (bytes)
- `grpc.server.active_requests` - количество активных запросов
- `grpc.server.stream.messages` - количество стрим-сообщений

Все метрики включают атрибуты:
- `grpc.method` - полное имя метода
- `grpc.status` - статус код ответа
- `grpc.is_client_stream` - является ли клиентским стримом (для streaming)
- `grpc.is_server_stream` - является ли серверным стримом (для streaming)

## Использование

### Базовая инициализация

```go
import "github.com/arrowwhi/infrastructure/observability/metrics"

cfg := metrics.Config{
    Enabled:  true,
    Exporter: "prometheus",
}

mp, err := metrics.InitMeterProvider(context.Background(), cfg, "my-service", "1.0.0", "production")
if err != nil {
    log.Fatal(err)
}
defer mp.Shutdown(context.Background())
```

### Использование с HTTP сервером

```go
import (
    httpserver "github.com/arrowwhi/infrastructure/http/server"
    "github.com/arrowwhi/infrastructure/observability/metrics"
)

// Инициализация метрик
mp, err := metrics.InitMeterProvider(ctx, cfg.Metrics, "my-service", "1.0.0", "production")
if err != nil {
    log.Fatal(err)
}
defer mp.Shutdown(context.Background())

// Создание сервера с metrics middleware
srv, err := httpserver.New(
    cfg.Server,
    httpserver.WithMiddlewares(
        metrics.Middleware("my-service"),
    ),
)

// Регистрация /metrics эндпоинта для Prometheus (если используется Prometheus экспортер)
if metricsHandler := metrics.Handler(); metricsHandler != nil {
    srv.Router().Handle("GET /metrics", metricsHandler)
}
```

**Важно:** При использовании Prometheus экспортера необходимо зарегистрировать HTTP handler на `/metrics`. 
Функция `metrics.Handler()` возвращает готовый handler, который нужно добавить в роутер сервера.

### Использование с gRPC сервером

```go
import (
    "github.com/arrowwhi/infrastructure/observability/metrics"
    "google.golang.org/grpc"
)

// Unary interceptor
unaryInterceptor := metrics.UnaryServerInterceptor("my-service")

// Stream interceptor
streamInterceptor := metrics.StreamServerInterceptor("my-service")

// Создание gRPC сервера с interceptors
srv := grpc.NewServer(
    grpc.ChainUnaryInterceptor(unaryInterceptor),
    grpc.ChainStreamInterceptor(streamInterceptor),
)
```

### Создание кастомных метрик

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
)

meter := otel.Meter("my-service")

// Counter
counter, _ := meter.Int64Counter(
    "custom.counter",
    metric.WithDescription("Custom counter metric"),
)
counter.Add(ctx, 1)

// Histogram
histogram, _ := meter.Float64Histogram(
    "custom.histogram",
    metric.WithDescription("Custom histogram metric"),
)
histogram.Record(ctx, 42.0)

// Gauge (UpDownCounter)
gauge, _ := meter.Int64UpDownCounter(
    "custom.gauge",
    metric.WithDescription("Custom gauge metric"),
)
gauge.Add(ctx, 1)
```

## Конфигурация

### Prometheus

```go
cfg := metrics.Config{
    Enabled:  true,
    Exporter: "prometheus",
}
```

После инициализации MeterProvider необходимо зарегистрировать HTTP handler:

```go
// В HTTP сервере
if metricsHandler := metrics.Handler(); metricsHandler != nil {
    srv.Router().Handle("GET /metrics", metricsHandler)
}
```

Метрики будут доступны по адресу `http://<server-host>:<server-port>/metrics`

### OTLP gRPC

```go
cfg := metrics.Config{
    Exporter:     "otlp-grpc",
    OTLPEndpoint: "localhost:4317",
    OTLPInsecure: true,
    MetricInterval: 60, // экспорт каждые 60 секунд
}
```

### OTLP HTTP

```go
cfg := metrics.Config{
    Exporter:     "otlp-http",
    OTLPEndpoint: "localhost:4318",
    OTLPInsecure: true,
    MetricInterval: 60,
}
```

## Интеграция с наблюдаемостью

Пакет разработан для совместной работы с другими компонентами observability:
- `observability/logger` - структурированное логирование
- `observability/tracing` - распределенная трассировка

Все три компонента используют OpenTelemetry для унифицированного подхода к наблюдаемости.

