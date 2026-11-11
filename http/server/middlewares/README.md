# http/server/middlewares

Стандартные `net/http`-middleware, которые `http/server` подключает к каждому роутеру сам:
идентификатор запроса, реальный IP клиента за прокси и access-лог через `observability/logger`.
Отдельно пакет нужен, если вы собираете свой роутер без `http/server` или хотите из обработчика
передать причину 5xx в лог (`SetError`).

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import "github.com/arrowwhi/infrastructure/http/server/middlewares"
```

## Пример

```go
package main

import (
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/arrowwhi/infrastructure/http/server/middlewares"
)

func main() {
	r := chi.NewRouter()
	r.Use(middlewares.RequestID, middlewares.RealIP, middlewares.Log)

	r.Get("/orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := r.Context().Value(middlewares.RequestIDKey).(string)
		_ = requestID

		if err := errors.New("db is down"); err != nil {
			middlewares.SetError(r, err) // попадёт в access-лог как error
			http.Error(w, "internal error", http.StatusInternalServerError)

			return
		}
	})

	log.Fatal(http.ListenAndServe(":8080", r))
}
```

## Что делает каждая

| Middleware | Поведение |
|---|---|
| `RequestID` | берёт `X-Request-ID` из запроса или генерирует UUID v4; кладёт в контекст под `RequestIDKey` |
| `RealIP` | выставляет `r.RemoteAddr` из `True-Client-IP` → `X-Real-IP` → первого адреса `X-Forwarded-For` (если это валидный IP) |
| `Log` / `NewLog(LogOptions)` | одна запись `incoming HTTP request` после ответа: info (<400), warn (4xx), error (5xx; с ошибкой из `SetError`, если она есть) |

Поля access-лога: `remote_addr`, `path`, `protocol`, `method`, `user_agent`, `status`, `status_code`,
`latency`, `bytes_in`, `request_id`, `request_body`, `response_body` (JSON-тела встраиваются как
объект, остальные — строкой; каждое до 512 КБ). Маскирование `observability/logger` применяется
к этим полям так же, как к остальным.

## Конфигурация

`Log` — access-лог с настройками по умолчанию (тела логируются). Для настройки — `NewLog`:

```go
r.Use(middlewares.NewLog(middlewares.LogOptions{DisableBodies: true}))
```

| Поле `LogOptions` | По умолчанию | Смысл |
|---|---|---|
| `DisableBodies` | `false` | не писать `request_body` / `response_body`; тело запроса не читается заранее, тело ответа не копится в буфер |

В `http/server` это поле выставляется из `Config.DisableBodyLogging` (`HTTP_DISABLE_BODY_LOGGING`).

## Связь с другими пакетами

- `http/server.New` подключает `Recoverer`, `RequestID`, `RealIP`, `NewLog(LogOptions{DisableBodies: cfg.DisableBodyLogging})`
  в этом порядке.
- Логи пишутся через `observability/logger` (уровень, маскирование, формат — из его конфигурации).

## Подводные камни

- `RealIP` доверяет заголовкам безусловно. Без доверенного прокси перед сервисом клиент может
  подставить любой IP.
- `RequestID` не возвращает `X-Request-ID` в ответе и не проверяет формат входящего значения.
- `Log` читает тело запроса в память (до 512 КБ + 1 байт) и буферизует тело ответа (кроме режима
  `DisableBodies`): не подходит для больших загрузок и стриминга; `ResponseWriter` не реализует `http.Flusher`, поэтому SSE
  через него не работает.
- `trace_id` попадает в access-лог только если спан создан **до** `Log` (в `http/server` это не так).
- Если обработчик пишет тело без `WriteHeader`, в лог попадает `status_code: 0`.
- `SetError` меняет `*r` на месте, и `Log` увидит ошибку, только если между ним и обработчиком никто
  не заменил запрос через `r.WithContext`. `tracing.Middleware` (otelhttp) заменяет — с ним ошибка
  в access-лог не попадёт, хотя уровень `error` по статусу останется.
