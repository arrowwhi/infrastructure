# http/client

HTTP-клиент с fluent API (`client.Get(ctx, path).SetQueryParam(...).Execute(&out)`), повторами
с экспоненциальной задержкой и джиттером, (де)сериализацией JSON/XML и своих форматов,
типизированными ошибками `HTTPError`, TLS/mTLS и инструментацией OpenTelemetry
(спан на каждую попытку, проброс `traceparent`, метрики `http.client.*`).

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import httpclient "github.com/arrowwhi/infrastructure/http/client"
```

## Пример

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	httpclient "github.com/arrowwhi/infrastructure/http/client"
)

type Post struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	UserID int    `json:"userId"`
}

func main() {
	ctx := context.Background()

	cfg := httpclient.Config{
		BaseURL:               "https://jsonplaceholder.typicode.com",
		DialTimeout:           5 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		Retry: &httpclient.RetryConfig{
			MaxRetries:      3,
			BackoffTime:     100 * time.Millisecond,
			BackoffFraction: 0.3,
		},
		ServiceName:    "posts-api",
		TracingEnabled: true,
		MetricsEnabled: true,
	}

	client, err := httpclient.NewClient(cfg, httpclient.WithHeader("Accept", "application/json"))
	if err != nil {
		log.Fatal(err)
	}

	var post Post
	resp, err := client.Get(ctx, "/posts/1").SetQueryParam("full", true).Execute(&post)
	if err != nil {
		if httpErr, ok := httpclient.AsHTTPError(err); ok {
			log.Fatalf("HTTP %d: %s", httpErr.StatusCode, httpErr.ResponseBody)
		}
		log.Fatal(err)
	}
	fmt.Println(resp.StatusCode, post.Title)

	var created Post
	_, err = client.Post(ctx, "/posts").
		SetHeader("Content-Type", "application/json").
		SetBody(Post{Title: "foo", UserID: 1}).
		Execute(&created)
	if err != nil {
		log.Fatal(err)
	}
}
```

- Относительный `path` склеивается с `BaseURL`; полный URL (`https://other.host/x`) используется как есть.
- Сериализация тела выбирается по заголовку `Content-Type` запроса, десериализация — по
  `Content-Type` ответа. Из коробки: `application/json`, `application/xml`; свои —
  `httpclient.WithSerializer(contentType, fn)` / `WithDeserializer(contentType, fn)`.
  `Execute(&b)` с `b []byte` отдаёт сырое тело, если для `Content-Type` ответа нет десериализатора.
  Заголовки клиента (`WithHeader`, `WithHeaders`) копируются в каждый запрос.
- Ответ со статусом ≥ 400 возвращается как `*HTTPError` (`StatusCode`, `URL`, `ResponseBody`).
- Логи — через интерфейс `Logger` (`httpclient.WithLogger`, по умолчанию noop).

## Конфигурация

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `HTTP_CLIENT_BASE_URL` | — | базовый URL |
| `HTTP_CLIENT_DIAL_TIMEOUT` | `30s` | установка TCP-соединения |
| `HTTP_CLIENT_TLS_HANDSHAKE_TIMEOUT` | `10s` | TLS-рукопожатие |
| `HTTP_CLIENT_RESPONSE_HEADER_TIMEOUT` | `30s` | ожидание заголовков ответа |
| `HTTP_CLIENT_MAX_IDLE_CONNS`, `HTTP_CLIENT_IDLE_CONN_TIMEOUT` | `10`, `90s` | пул соединений |
| `HTTP_CLIENT_RETRY_MAX_RETRIES` | `3` | повторов после первой попытки |
| `HTTP_CLIENT_RETRY_BACKOFF_TIME`, `HTTP_CLIENT_RETRY_BACKOFF_FRACTION` | `100ms`, `0.3` | база экспоненциальной задержки и доля джиттера |
| `HTTP_CLIENT_TLS_INSECURE_SKIP_VERIFY` | `false` | не проверять сертификат сервера |
| `HTTP_CLIENT_TLS_ROOT_CAS`, `HTTP_CLIENT_TLS_CLIENT_CERT`, `HTTP_CLIENT_TLS_CLIENT_KEY` | — | свой CA и клиентский сертификат (PEM-содержимое) |
| `HTTP_CLIENT_SERVICE_NAME` | `http-client` | имя meter'а |
| `HTTP_CLIENT_TRACING_ENABLED` | `true` | спаны и `traceparent` |
| `HTTP_CLIENT_METRICS_ENABLED` | `true` | метрики `http.client.*` |

## Метрики, трейсы, логи

- Транспорт оборачивается в `otelhttp`: каждая попытка (включая повторы) — отдельный спан
  `HTTP GET api.example.com`, заголовок `traceparent` уходит на сервер.
- Метрики otelhttp по семантическим соглашениям OTel: `http.client.request.duration` (с),
  `http.client.request.body.size`, `http.client.response.body.size`.
- `http.client.retry.count` с меткой `http.request.method` — число повторов.
- Логи: только через ваш `Logger` (повторы — `Warning`).

## Связь с другими пакетами

- Трейсы и метрики уходят в глобальные провайдеры: инициализируйте `observability/tracing` и
  `observability/metrics` (или `telegram/app`) до создания клиента.
- Входящий `ctx` обработчика `http/server` с трейсингом даёт сквозной трейс: спан клиента
  становится дочерним.

## Подводные камни

- `env-default` работает только при загрузке через cleanenv (`app.LoadConfig`). `Config`, собранный
  литералом, получает нули: таймауты выключены, `TracingEnabled`/`MetricsEnabled` = `false`,
  `Retry: nil` — без повторов.
- Повторяются **любые** методы, включая `POST`, при сетевых ошибках и статусах 429, 500, 502, 503, 504.
  Для неидемпотентных запросов выставляйте `MaxRetries: 0` или используйте ключи идемпотентности.
  Заголовок `Retry-After` не учитывается.
- `SetBody(v)` без подходящего `Content-Type` (ни у запроса, ни у клиента) требует `v` типа `[]byte`;
  структура без `Content-Type: application/json` приведёт к панике.
- Общего таймаута на запрос нет — задавайте дедлайн через `ctx`.
- Для `HTTP_CLIENT_BASE_URL` стоит тег `required`, который cleanenv не проверяет: пустое значение
  не даст ошибки при загрузке.
