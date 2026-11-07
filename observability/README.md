# Observability Stack

Полный стек наблюдаемости для мониторинга приложений: логи, трейсы и метрики.

## Компоненты

### 1. OpenTelemetry Collector
Принимает трейсы и метрики через OTLP протокол (gRPC/HTTP) и экспортирует их в Jaeger и Prometheus.

### 2. Jaeger
Система распределенной трассировки для анализа производительности и отладки.

### 3. Prometheus
Система мониторинга и база данных временных рядов для хранения метрик.

### 4. Grafana
Платформа визуализации для создания дашбордов с метриками и трейсами.

## Быстрый старт

### 1. Запуск стека

```bash
# Из корня проекта
docker-compose up -d

# Проверить статус
docker-compose ps

# Посмотреть логи
docker-compose logs -f
```

### 2. Запуск примера HTTP сервера

```bash
# Скомпилировать и запустить
go run ./examples/http/main.go
```

### 3. Генерация тестовых данных

```bash
# GET запросы
curl http://localhost:8080/api/users/123
curl http://localhost:8080/api/users/456

# POST запросы
curl -X POST http://localhost:8080/api/orders \
  -H "Content-Type: application/json" \
  -d '{"product_id": 1, "quantity": 2}'

# Запрос с ошибкой
curl http://localhost:8080/api/error

# Запрос к внешнему сервису
curl http://localhost:8080/api/external

# Параллельные операции
curl http://localhost:8080/api/parallel
```

### 4. Нагрузочное тестирование

```bash
# Установить hey (если нет)
go install github.com/rakyll/hey@latest

# Запустить нагрузку
hey -z 30s -c 10 http://localhost:8080/api/users/123
```

## Доступ к интерфейсам

### Grafana
- **URL:** http://localhost:3000
- **Логин:** admin
- **Пароль:** admin
- **Дашборд:** HTTP Server Metrics (автоматически загружен)

### Prometheus
- **URL:** http://localhost:9090
- **Проверка targets:** http://localhost:9090/targets
- **Запросы:** http://localhost:9090/graph

### Jaeger UI
- **URL:** http://localhost:16686
- **Поиск трейсов:** выбрать сервис "http-server-example"

### OpenTelemetry Collector
- **Метрики коллектора:** http://localhost:8888/metrics
- **Health check:** http://localhost:13133

## Основные метрики

### HTTP Server Metrics (автоматические)

#### otel_http_server_request_count
Счетчик HTTP запросов
```promql
# Rate запросов в секунду
rate(otel_http_server_request_count_total[5m])

# Запросы по статус-кодам
sum by(http_status_code) (rate(otel_http_server_request_count_total[5m]))

# Запросы по эндпоинтам
sum by(http_route) (rate(otel_http_server_request_count_total[5m]))
```

#### otel_http_server_request_duration
Histogram длительности запросов (ms)
```promql
# p50, p95, p99 латентность
histogram_quantile(0.50, sum(rate(otel_http_server_request_duration_bucket[5m])) by (le))
histogram_quantile(0.95, sum(rate(otel_http_server_request_duration_bucket[5m])) by (le))
histogram_quantile(0.99, sum(rate(otel_http_server_request_duration_bucket[5m])) by (le))

# Средняя длительность
rate(otel_http_server_request_duration_sum[5m]) / rate(otel_http_server_request_duration_count[5m])
```

#### otel_http_server_active_requests
Gauge активных запросов
```promql
# Текущее количество активных запросов
otel_http_server_active_requests

# Максимум за период
max_over_time(otel_http_server_active_requests[5m])
```

#### otel_http_server_request_size / otel_http_server_response_size
Histogram размеров запросов/ответов (bytes)
```promql
# Средний размер запросов
rate(otel_http_server_request_size_sum[5m]) / rate(otel_http_server_request_size_count[5m])

# Средний размер ответов
rate(otel_http_server_response_size_sum[5m]) / rate(otel_http_server_response_size_count[5m])
```

### Application Business Metrics (кастомные)

#### otel_app_user_requests
Счетчик запросов к API пользователей
```promql
rate(otel_app_user_requests_total[5m])
```

#### otel_app_orders_created
Счетчик созданных заказов
```promql
rate(otel_app_orders_created_total[5m])
sum(otel_app_orders_created_total)  # Всего заказов
```

#### otel_app_errors
Счетчик ошибок
```promql
rate(otel_app_errors_total[5m])
sum by(error_type) (rate(otel_app_errors_total[5m]))
```

#### otel_app_processing_duration
Histogram времени обработки операций (ms)
```promql
# Латентность по операциям
histogram_quantile(0.95, sum by(le, operation) (rate(otel_app_processing_duration_bucket[5m])))
```

## Создание кастомных метрик

### Counter

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
)

meter := otel.Meter("my-service")

counter, _ := meter.Int64Counter(
    "custom.events",
    metric.WithDescription("Number of custom events"),
    metric.WithUnit("{event}"),
)

counter.Add(ctx, 1,
    metric.WithAttributes(
        attribute.String("event.type", "user_login"),
    ),
)
```

### Histogram

```go
histogram, _ := meter.Float64Histogram(
    "custom.duration",
    metric.WithDescription("Custom operation duration"),
    metric.WithUnit("ms"),
)

start := time.Now()
// ... do work ...
duration := float64(time.Since(start).Milliseconds())
histogram.Record(ctx, duration)
```

### Gauge (UpDownCounter)

```go
gauge, _ := meter.Int64UpDownCounter(
    "custom.queue.size",
    metric.WithDescription("Queue size"),
    metric.WithUnit("{item}"),
)

gauge.Add(ctx, 5)   // добавить
gauge.Add(ctx, -3)  // убавить
```

## Алерты в Prometheus

Создайте файл `alerts.yml`:

```yaml
groups:
  - name: http_server
    interval: 30s
    rules:
      - alert: HighErrorRate
        expr: |
          sum(rate(otel_http_server_request_count_total{http_status_code=~"5.."}[5m])) 
          / 
          sum(rate(otel_http_server_request_count_total[5m])) 
          > 0.05
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High error rate detected"
          description: "Error rate is {{ $value | humanizePercentage }}"

      - alert: HighLatency
        expr: |
          histogram_quantile(0.95, 
            sum(rate(otel_http_server_request_duration_bucket[5m])) by (le)
          ) > 1000
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High latency detected"
          description: "P95 latency is {{ $value }}ms"
```

## Troubleshooting

### Метрики не появляются в Prometheus

1. Проверьте targets:
```bash
curl http://localhost:9090/api/v1/targets
```

2. Проверьте логи OTel Collector:
```bash
docker-compose logs otel-collector
```

3. Проверьте что приложение отправляет метрики:
```bash
# Должны быть логи "Exporting metrics"
docker-compose logs otel-collector | grep metrics
```

### Трейсы не появляются в Jaeger

1. Проверьте подключение к OTel Collector:
```bash
curl http://localhost:4317
```

2. Проверьте конфигурацию в приложении:
```go
tracingCfg := tracing.Config{
    OTLPEndpoint: "localhost:4317",  // Должно совпадать с портом коллектора
    OTLPInsecure: true,
}
```

### Grafana не показывает данные

1. Проверьте datasources:
- Откройте http://localhost:3000/datasources
- Проверьте что Prometheus и Jaeger подключены
- Нажмите "Test" для проверки соединения

2. Проверьте запросы в дашборде:
- Откройте панель → Edit
- Проверьте что метрики существуют в Prometheus

## Остановка стека

```bash
# Остановить все сервисы
docker-compose down

# Остановить с удалением volumes
docker-compose down -v
```

## Структура файлов

```
observability/
├── README.md                              # Эта инструкция
├── logger/                                # Пакет логирования
├── tracing/                               # Пакет трассировки
└── metrics/                               # Пакет метрик

examples/
├── docker-compose.yml                     # Стек: otel-collector, jaeger, prometheus, grafana
├── otel-collector-config.yml              # Конфиг OpenTelemetry Collector
├── prometheus.yml                         # Конфиг Prometheus
└── grafana/
    └── provisioning/
        ├── datasources/
        │   └── datasources.yml            # Auto-provisioning источников данных
        └── dashboards/
            ├── dashboards.yml             # Auto-provisioning дашбордов
            └── json/
                └── http-server-metrics.json  # Готовый дашборд
```

## Best Practices

1. **Именование метрик:** используйте понятные имена с префиксом приложения
2. **Атрибуты:** добавляйте полезные атрибуты, но избегайте high cardinality
3. **Sampling:** в проде используйте sampling для трейсов (0.1-0.01)
4. **Метрики vs Трейсы:** используйте метрики для агрегированных данных, трейсы для детального анализа
5. **Дашборды:** создавайте separate дашборды для разных уровней (инфраструктура, приложение, бизнес)

