# Quick Start - Observability Stack

Быстрый запуск полного стека наблюдаемости с метриками, трейсами и визуализацией.

## 1. Запуск стека (одной командой)

```bash
make run-example
```

Эта команда:
- Запустит OpenTelemetry Collector, Jaeger, Prometheus, Grafana
- Дождется их готовности
- Запустит HTTP сервер с примером

## 2. Откройте интерфейсы

### Grafana (метрики + дашборды)
```
http://localhost:3000
Логин: admin
Пароль: admin
```

Дашборд автоматически загружен: **HTTP Server Metrics**

### Prometheus (raw метрики)
```
http://localhost:9090
```

### Jaeger (трейсы)
```
http://localhost:16686
```

Выберите сервис: `http-server-example`

## 3. Сгенерируйте трафик

В новом терминале:

```bash
# Простые запросы
make test-endpoints

# Нагрузочное тестирование (30 сек)
make load-test
```

Или вручную:

```bash
# GET запросы
curl http://localhost:8080/api/users/123
curl http://localhost:8080/api/users/456

# POST запрос
curl -X POST http://localhost:8080/api/orders \
  -H "Content-Type: application/json" \
  -d '{"product_id": 1}'

# Запрос с ошибкой
curl http://localhost:8080/api/error

# Параллельные операции
curl http://localhost:8080/api/parallel
```

## 4. Просмотр метрик

### В Grafana
1. Открыть http://localhost:3000
2. Зайти в **Dashboards → HTTP Server Metrics**
3. Увидеть:
   - Request Rate (запросов/сек)
   - Active Requests
   - Request Duration (латентность p50/p95/p99)
   - HTTP Status Codes
   - Business Metrics (user requests, orders created, errors)
   - Processing Duration по операциям

### В Prometheus
1. Открыть http://localhost:9090
2. Примеры запросов:

```promql
# Rate запросов
rate(otel_http_server_request_count_total[5m])

# P95 латентность
histogram_quantile(0.95, sum(rate(otel_http_server_request_duration_bucket[5m])) by (le))

# Активные запросы
otel_http_server_active_requests

# Ошибки
sum by(http_status_code) (rate(otel_http_server_request_count_total{http_status_code=~"5.."}[5m]))
```

## 5. Просмотр трейсов

### В Jaeger
1. Открыть http://localhost:16686
2. Выбрать **Service: http-server-example**
3. Нажать **Find Traces**
4. Кликнуть на любой трейс для детального просмотра
5. Увидеть:
   - Timeline всех операций
   - Вложенные spans
   - Атрибуты и события
   - Связанные логи

## 6. Управление стеком

```bash
# Запустить стек
make obs-up

# Остановить стек
make obs-down

# Посмотреть статус
make obs-status

# Посмотреть логи
make obs-logs

# Очистить все данные
make obs-clean

# Запустить только сервер (стек должен быть запущен)
make run-server
```

## Доступные метрики

### HTTP Server (автоматические)
- `otel_http_server_request_count` - количество запросов
- `otel_http_server_request_duration` - латентность (histogram)
- `otel_http_server_request_size` - размер запросов
- `otel_http_server_response_size` - размер ответов
- `otel_http_server_active_requests` - активные запросы

### Application (бизнес-метрики)
- `otel_app_user_requests` - запросы к пользователям
- `otel_app_orders_created` - созданные заказы
- `otel_app_errors` - ошибки
- `otel_app_processing_duration` - время обработки операций

## Troubleshooting

### Метрики не появляются
```bash
# Проверить статус
make obs-status

# Проверить логи коллектора
make obs-logs-otel

# Проверить targets в Prometheus
open http://localhost:9090/targets
```

### Порты заняты
```bash
# Остановить все
make obs-down

# Проверить занятые порты
lsof -i :3000  # Grafana
lsof -i :9090  # Prometheus
lsof -i :4317  # OTel
lsof -i :16686 # Jaeger
```

### Очистить все данные
```bash
make obs-clean
```

## Что дальше?

- Изучите [полную документацию](./observability/README.md)
- Настройте [алерты в Prometheus](./observability/README.md#алерты-в-prometheus)
- Создайте [кастомные метрики](./observability/metrics/README.md#создание-кастомных-метрик)
- Интегрируйте в свое приложение

## Архитектура

```
┌─────────────┐
│ Ваше        │ OTLP gRPC
│ приложение  ├──────────┐
└─────────────┘          │
                         ▼
              ┌──────────────────┐
              │ OTel Collector   │
              └────┬────────┬────┘
                   │        │
         ┌─────────┘        └──────────┐
         ▼                             ▼
    ┌─────────┐                  ┌──────────┐
    │ Jaeger  │                  │Prometheus│
    │ (traces)│                  │ (metrics)│
    └─────────┘                  └────┬─────┘
                                      │
                                      ▼
                                 ┌─────────┐
                                 │ Grafana │
                                 │  (viz)  │
                                 └─────────┘
```

## Использование в продакшене

1. **Sampling:** уменьшите `SamplingRate` до 0.01-0.1
2. **Resources:** добавьте лимиты в docker-compose
3. **Persistence:** настройте volumes для данных
4. **Security:** включите authentication
5. **Alerting:** настройте Alertmanager
6. **Retention:** настройте retention в Prometheus

Подробнее в [документации](./observability/README.md).

