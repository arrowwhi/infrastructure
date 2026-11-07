# Telegram

Скелет Telegram-бота: всё, что повторяется в каждом боте (запуск, конфиг, лимиты Telegram, порядок
обработки, шаги диалога, метрики и трейсы), уже собрано — остаётся написать обработчики. Всё лежит
в одном пакете `telegram`.

| Часть | Что делает |
|---|---|
| [Запуск сервиса](#запуск-сервиса) | Конфиг из env, логгер, метрики, трейсинг, health-проверки, служебный порт, остановка в обратном порядке |
| [Бот](#бот) | Роутер команд/кнопок/текста, шаги диалога, воркеры с порядком внутри чата, polling и webhook |
| [Клиент Bot API](#клиент-bot-api) | Рукописный клиент Bot API: лимиты Telegram, повтор после 429, метрики и трейсы, токен не попадает в ошибки |
| [Хранилище шагов диалога](#хранилище-шагов-диалога) | Шаги диалога в памяти, PostgreSQL или Redis |

Как они связаны: `App` поднимает инфраструктуру и запускает `Bot` как компонент; `Bot` получает
обновления через `Client` и хранит шаги диалога в `Store`; `Client` можно использовать и без бота
(рассылки из воркера). `App` не привязан к Telegram — им удобно запускать любой сервис.

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import "github.com/arrowwhi/infrastructure/telegram"
```

## Минимальный бот

```go
package main

import (
	"context"
	"log"

	"github.com/arrowwhi/infrastructure/telegram"
)

type Config struct {
	App      telegram.AppConfig
	Telegram telegram.Config
}

func main() {
	ctx := context.Background()

	var cfg Config
	if err := telegram.LoadConfig(&cfg); err != nil {
		log.Fatal(err)
	}

	a, err := telegram.NewApp(ctx, cfg.App)
	if err != nil {
		log.Fatal(err)
	}

	b, err := telegram.New(cfg.Telegram)
	if err != nil {
		log.Fatal(err)
	}

	b.Command("start", "Начать", func(c *telegram.Context) error {
		_, err := c.Reply("Привет!")
		return err
	})

	a.Health().Register("telegram", b.Healthcheck)

	if err = a.Run(ctx, b); err != nil {
		log.Fatal(err)
	}
}
```

```bash
SERVICE_NAME=my-bot TELEGRAM_BOT_TOKEN=123:abc go run .
# метрики и пробы: http://localhost:9090/metrics, /healthz, /ready
```

Рабочий пример с диалогом, кнопками и выбором хранилища — [`examples/tgbot`](../examples/tgbot/main.go).

## Старые пакеты `telegram/app`, `telegram/bot`, `telegram/api`, `telegram/state`

Раньше части жили в отдельных пакетах. Они остались и продолжают собираться, но помечены
`Deprecated`: внутри только алиасы типов и функции-обёртки над `telegram`. Типы — те же самые
(`bot.Context` и `telegram.Context` — один тип), поэтому старый и новый код можно смешивать и
переезжать постепенно. При переезде часть имён получила префикс, чтобы не конфликтовать в одном пакете:

| Было | Стало |
|---|---|
| `app.Config`, `app.New` | `telegram.AppConfig`, `telegram.NewApp` |
| `api.Config`, `api.DefaultConfig`, `api.Option`, `api.New`, `api.Error` | `telegram.ClientConfig`, `telegram.DefaultClientConfig`, `telegram.ClientOption`, `telegram.NewClient`, `telegram.APIError` |
| `state.Key`, `state.New`, `state.ErrNotFound` | `telegram.StateKey`, `telegram.NewState`, `telegram.ErrStateNotFound` |
| `state.Memory`, `state.NewMemory` | `telegram.MemoryStore`, `telegram.NewMemoryStore` |
| `state.Postgres`, `state.NewPostgres`, `state.PostgresSchema` | `telegram.PostgresStore`, `telegram.NewPostgresStore`, `telegram.PostgresStateSchema` |
| `state.Redis`, `state.NewRedis`, `state.DefaultRedisPrefix` | `telegram.RedisStore`, `telegram.NewRedisStore`, `telegram.DefaultRedisStatePrefix` |
| всё остальное (`bot.*` целиком, `api.SendMessageRequest`, `app.Component`…) | то же имя в `telegram` |

Названия метрик, спанов и переменных окружения не менялись.

## Запуск сервиса

Каркас запуска сервиса (не только бота): конфиг из env/`.env` одним вызовом, логгер, трейсинг
и метрики (глобально — все коннекторы библиотеки подхватывают их сами), реестр health-проверок,
служебный порт с `/healthz`, `/ready`, `/metrics` и pprof, запуск долгоживущих компонентов и
остановка в правильном порядке по SIGINT/SIGTERM.

### Пример

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/arrowwhi/infrastructure/telegram"
)

type Config struct {
	App      telegram.AppConfig
	Postgres postgres.Config
}

func main() {
	ctx := context.Background()

	var cfg Config
	if err := telegram.LoadConfig(&cfg); err != nil {
		log.Fatal(err)
	}

	a, err := telegram.NewApp(ctx, cfg.App) // логгер, трейсинг, метрики, health
	if err != nil {
		log.Fatal(err)
	}

	pool, err := postgres.New(ctx, cfg.Postgres)
	if err != nil {
		log.Fatal(err)
	}
	a.Health().Register("postgres", pool.Healthcheck)
	a.OnShutdown(func(context.Context) error { pool.Close(); return nil })

	worker := telegram.ComponentFunc(func(ctx context.Context) error {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				logger.Info(ctx).Msg("tick")
			}
		}
	})

	if err = a.Run(ctx, worker); err != nil { // блокируется до сигнала или ошибки компонента
		log.Fatal(err)
	}
}
```

```bash
SERVICE_NAME=my-service DB_USER=svc DB_DATABASE=app go run .
```

Компонент — всё, что реализует `Run(ctx context.Context) error` и возвращает `nil` при отмене `ctx`:
`*telegram.Bot`, `*httpserver.Server`, `*grpcserver.Server` или функция через `telegram.ComponentFunc`.

### Конфигурация

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `SERVICE_NAME` | — | **обязательна**; имя в логах, метриках, трейсах |
| `SERVICE_VERSION`, `ENVIRONMENT` | `dev`, `local` | версия и окружение там же |
| `SHUTDOWN_TIMEOUT` | `30s` | сколько ждать остановки компонентов, `OnShutdown` и сброса телеметрии |
| `ADMIN_ENABLED` | `true` | служебный HTTP-порт |
| `ADMIN_HOST`, `ADMIN_PORT` | `0.0.0.0`, `9090` | его адрес |
| `ADMIN_PPROF_ENABLED` | `false` | `/debug/pprof/*` |
| `LOG_*` | см. [`observability/logger`](../observability/logger/README.md) | `LOG_LEVEL=info`, `LOG_PRETTYOUTPUT=false`, `LOG_MASKING=true` |
| `METRICS_*` | см. [`observability/metrics`](../observability/metrics/README.md) | `METRICS_EXPORTER=prometheus`, `METRICS_ENABLED=true`… |
| `OTEL_*` | см. [`observability/tracing`](../observability/tracing/README.md) | `OTEL_ENABLED=false`, `OTEL_EXPORTER=otlp-grpc`, `OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317` |

`telegram.LoadConfig(&cfg)` заполняет любую структуру по тегам `env`, `env-default`, `env-prefix`
([cleanenv](https://github.com/ilyakaznacheev/cleanenv)) — поля `telegram.AppConfig`, `postgres.Config`,
`redis.Config`, `telegram.Config` можно класть в один конфиг сервиса рядом с собственными. Отсутствие
`env-required`-поля — ошибка.

### Служебный порт

| Путь | |
|---|---|
| `/healthz` | процесс жив; зависимости не проверяет — иначе падение базы перезапустит все поды |
| `/ready` | результат `a.Health()`, 503 при падении критичной проверки |
| `/metrics` | при `METRICS_EXPORTER=prometheus` |
| `/debug/pprof/*` | при `ADMIN_PPROF_ENABLED=true` |

Порт не публикуют наружу: на него смотрят Prometheus и пробы Kubernetes. `a.AdminHandler()` отдаёт
тот же обработчик для тестов или встраивания.

### Метрики, трейсы, логи

Сам пакет пишет `health.check.status`/`health.check.duration` (через `Health().RegisterMetrics`),
`app_build_info` (из `observability/metrics`) и логи старта/остановки. Всё остальное — компоненты.

### Порядок запуска и остановки

1. `telegram.NewApp`: логгер → трейсинг → метрики → health-метрики.
2. `a.Run`: служебный сервер и все компоненты параллельно.
3. Остановка по SIGINT/SIGTERM, отмене `ctx` или первой ошибке любого компонента: компоненты
   получают отменённый `ctx` → функции `OnShutdown` в **обратном** порядке регистрации
   (созданное последним закрывается первым) → сброс метрик и трейсов.
   `Run` возвращает первую ошибку компонента вместе с ошибками остановки.

### Связь с другими частями и пакетами

- [бот](#бот): `a.Run(ctx, b)` и `a.Health().Register("telegram", b.Healthcheck)`.
- [`http/server`](../http/server/README.md), [`grpc/server`](../grpc/server/README.md) — тоже компоненты;
  передавайте им `a.Health()` (`WithHealthRegistry`), чтобы проверки были одни на весь сервис.
- Коннекторы ([`connections/postgres`](../connections/postgres/README.md),
  [`connections/redis`](../connections/redis/README.md)) регистрируют проверку в `a.Health()` и закрытие в `a.OnShutdown`.

### Подводные камни

- `.env` из рабочего каталога читается первым и его значения **перекрывают** переменные окружения
  процесса (cleanenv делает `os.Setenv` для каждой строки). Не кладите `.env` в образ для прода.
- Пустая переменная (`ADMIN_PORT=`) в окружении или в `.env` — это значение, а не «не задано»:
  `env-default` не применится.
- `SHUTDOWN_TIMEOUT` читают и `telegram.AppConfig`, и `tracing.Config` (у трейсинга нет префикса).
- `telegram.NewApp` вызывает `logger.Init`, а он срабатывает один раз на процесс: второй `telegram.NewApp` в тестах
  не поменяет уровень логов. Prometheus-экспортер тоже регистрируется глобально один раз — в тестах
  выставляйте `cfg.Metrics.Enabled = false`.
- Компонент, который возвращается сразу (не блокируется до отмены `ctx`) с `nil`, не останавливает
  приложение, но и не работает — `Run` должен блокироваться.

## Бот

Каркас Telegram-бота: роутер команд, кнопок и текста, шаги диалога с хранением в
[хранилище шагов диалога](#хранилище-шагов-диалога), параллельные воркеры со строгим порядком внутри чата,
получение обновлений через long polling или webhook, встроенные recovery, логи, метрики и трейсы
на каждое обновление. Бот реализует `telegram.Component` и запускается через [запуск сервиса](#запуск-сервиса).

### Пример

```go
package main

import (
	"context"
	"log"

	"github.com/arrowwhi/infrastructure/telegram"
)

type Config struct {
	App      telegram.AppConfig
	Telegram telegram.Config
}

type movie struct {
	Title string `json:"title"`
}

func main() {
	ctx := context.Background()

	var cfg Config
	if err := telegram.LoadConfig(&cfg); err != nil {
		log.Fatal(err)
	}

	a, err := telegram.NewApp(ctx, cfg.App)
	if err != nil {
		log.Fatal(err)
	}

	b, err := telegram.New(cfg.Telegram, telegram.WithErrorHandler(telegram.ReplyOnError("Что-то пошло не так")))
	if err != nil {
		log.Fatal(err)
	}

	b.Command("start", "Начать", func(c *telegram.Context) error {
		_, err := c.Reply("Привет! /rate — оценить фильм")
		return err
	})

	b.Command("rate", "Оценить фильм", func(c *telegram.Context) error {
		if err := c.SetState("awaiting_title", nil); err != nil {
			return err
		}
		_, err := c.Reply("Какой фильм?")
		return err
	})

	b.OnState("awaiting_title", func(c *telegram.Context) error {
		if err := c.SetState("awaiting_score", movie{Title: c.Text()}); err != nil { // данные переживают рестарт
			return err
		}
		kb := telegram.InlineKeyboard(telegram.Row(
			telegram.CallbackButton("👍", telegram.MustCallbackData("score", "10")),
			telegram.CallbackButton("👎", telegram.MustCallbackData("score", "1")),
		))
		_, err := c.Reply("Оценка?", telegram.WithKeyboard(kb))
		return err
	})

	b.Callback("score", func(c *telegram.Context) error {
		s, ok, err := c.State()
		if err != nil || !ok || s.Name != "awaiting_score" {
			return err
		}
		var m movie
		if err = s.Decode(&m); err != nil {
			return err
		}
		if err = c.ClearState(); err != nil {
			return err
		}
		return c.Edit("«"+m.Title+"» — "+c.CallbackArgs()[0]+"/10", nil, "")
	})

	a.Health().Register("telegram", b.Healthcheck)

	if err = a.Run(ctx, b); err != nil {
		log.Fatal(err)
	}
}
```

```bash
SERVICE_NAME=my-bot TELEGRAM_BOT_TOKEN=123:abc go run .
```

Полный пример с выбором хранилища состояний — [`examples/tgbot`](../examples/tgbot/main.go).

#### Маршруты

```go
b.Command("rate", "Оценить фильм", h) // /rate, /rate@my_bot, "/rate аргументы" → c.Args()
b.Callback("score", h)                // data "score" или "score:..." → c.CallbackArgs()
b.OnState("awaiting_title", h)        // сообщения, пока диалог в этом шаге
b.Text(h)                             // остальной текст
b.Default(h)                          // всё, что не подошло (иначе — только метрика unhandled)
b.Use(mw)                             // свои middleware (авторизация, лимиты пользователя…)
```

Порядок для сообщений: команда → шаг диалога → текст → default. Команды срабатывают в любом шаге,
поэтому `/start` всегда выводит пользователя из диалога. Для кнопок выбирается самый длинный
подходящий префикс. Команды с описанием публикуются в меню бота при старте.

Данные кнопок собирайте через `telegram.CallbackData(prefix, args...)` (ошибка при превышении 64 байт)
или `telegram.MustCallbackData` (паника — для констант).

#### Шаги диалога

`c.SetState(name, data)` сохраняет шаг и данные (JSON) для пары «пользователь + чат» на
`TELEGRAM_STATE_TTL`; `c.State()` читает, `c.ClearState()` завершает. Хранилище — опция
`telegram.WithStateStore(store)`, по умолчанию в памяти процесса (см. [хранилище шагов диалога](#хранилище-шагов-диалога)).

#### Ответы

`c.Reply(text, opts...)` с опциями `telegram.WithKeyboard`, `telegram.WithParseMode(telegram.ParseModeHTML)`,
`telegram.Silent()`, `telegram.WithoutPreview()`; `c.Edit(text, markup, parseMode)` — правка сообщения с кнопкой
(«message is not modified» не считается ошибкой); `c.Answer(text, alert)` — ответ на нажатие;
`c.API()` — полный клиент [клиент Bot API](#клиент-bot-api).

### Конфигурация

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` и другие `TELEGRAM_*` клиента | — | см. [клиент Bot API](#клиент-bot-api) |
| `TELEGRAM_MODE` | `polling` | `polling` или `webhook` |
| `TELEGRAM_POLL_TIMEOUT` | `30s` | long polling `getUpdates` |
| `TELEGRAM_WORKERS` | `8` | параллельных обработчиков |
| `TELEGRAM_QUEUE_SIZE` | `128` | очередь каждого воркера; при переполнении приём притормаживает |
| `TELEGRAM_HANDLER_TIMEOUT` | `30s` | дедлайн обработки одного обновления |
| `TELEGRAM_STATE_TTL` | `24h` | срок жизни шага диалога |
| `TELEGRAM_SET_COMMANDS` | `true` | публиковать меню команд (`setMyCommands`) |
| `TELEGRAM_WEBHOOK_URL` | — | публичный HTTPS-адрес с путём (webhook) |
| `TELEGRAM_WEBHOOK_SECRET` | — | секрет `X-Telegram-Bot-Api-Secret-Token`, обязателен в webhook |
| `TELEGRAM_WEBHOOK_HOST`, `TELEGRAM_WEBHOOK_PORT`, `TELEGRAM_WEBHOOK_PATH` | `0.0.0.0`, `8443`, `/telegram/webhook` | встроенный сервер; пустой порт — без него |
| `TELEGRAM_WEBHOOK_DROP_PENDING` | `false` | отбросить накопившиеся обновления при регистрации webhook |

| | `TELEGRAM_MODE=polling` | `TELEGRAM_MODE=webhook` |
|---|---|---|
| Где | локальная разработка | продакшен |
| Публичный адрес | не нужен | `TELEGRAM_WEBHOOK_URL` (HTTPS) |
| Безопасность | — | без `TELEGRAM_WEBHOOK_SECRET` бот не стартует; запросы без секрета — 401 |
| Сервер | — | встроенный на `TELEGRAM_WEBHOOK_PORT`; при пустом порте смонтируйте `b.WebhookHandler()` на свой роутер |

### Метрики, трейсы, логи

| Метрика | Метки | |
|---|---|---|
| `telegram.bot.updates.count` | `type`, `route`, `status` (`ok`, `error`, `panic`, `unhandled`) | обновления |
| `telegram.bot.update.duration` | `type`, `route` | время обработки, с |
| `telegram.bot.queue.size` | | ждут обработки; растёт — обработчики не успевают |

Плюс метрики клиента `telegram.api.*` (см. [клиент Bot API](#клиент-bot-api)).

Трейсы: спан `telegram.update <route>` на каждое обновление (атрибуты `telegram.update_id`,
`telegram.chat_id`, `telegram.user_id`, `telegram.route`), дочерние спаны `telegram.<method>` на
вызовы Bot API; запросы к базе и Redis из обработчика попадают в тот же трейс, если передавать им
`c` (он же `context.Context`). В webhook-режиме — ещё спан HTTP-запроса `telegram.webhook`.

Логи: ошибка обработчика и паника логируются с `trace_id`, попадают в метрики и трейс и передаются
в `ErrorHandler` (`telegram.WithErrorHandler`; готовый — `telegram.ReplyOnError(text)`).

### Связь с другими частями и пакетами

- [запуск сервиса](#запуск-сервиса): `a.Run(ctx, b)`, `a.Health().Register("telegram", b.Healthcheck)` (`getMe`).
- [хранилище шагов диалога](#хранилище-шагов-диалога): `telegram.WithStateStore(telegram.NewPostgresStore(...) / telegram.NewRedisStore(...))`.
- [клиент Bot API](#клиент-bot-api): `b.API()` — для рассылок вне обработчиков; `telegram.WithAPIClient(c)` — свой клиент.
- [`http/server`](../http/server/README.md): при `TELEGRAM_WEBHOOK_PORT=` — `srv.Router().Handle("/telegram/webhook", b.WebhookHandler())`.

### Подводные камни

- Обновления одного чата обрабатываются строго по очереди одним воркером: долгий обработчик
  задерживает следующие сообщения этого чата. Тяжёлую работу выносите из обработчика.
- На нажатие кнопки бот всегда отвечает (`answerCallbackQuery`), даже если обработчик забыл, —
  иначе крутятся «часики». Свой текст — `c.Answer(...)` в обработчике.
- В polling-режиме бот при старте снимает webhook (`deleteWebhook`). Запуск локально с продовым
  токеном отключит прод — заведите отдельного бота для разработки.
- Polling переживает rolling-деплой: пока старый экземпляр держит `getUpdates` (409 Conflict), новый
  ждёт без шума в логах. В webhook-режиме webhook при остановке не снимается.
- При остановке приём прекращается, начатые обработки завершаются не дольше `TELEGRAM_HANDLER_TIMEOUT`.
- Маршруты регистрируются до `Run`. `Username()` известен только после старта.
- Хранилище по умолчанию — в памяти: шаги диалога теряются при рестарте и не видны другим копиям бота.

## Клиент Bot API

Небольшой рукописный клиент Telegram Bot API без собственного диспетчера (роутинг — в
[боте](#бот)). Клиент сам соблюдает лимиты Telegram (глобальный и на чат),
повторяет запрос после 429 с паузой из `retry_after`, не пишет токен бота в ошибки и снимает метрики
и трейсы каждого вызова. Подходит и отдельно от бота — например, для рассылок из воркера.

### Пример

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/arrowwhi/infrastructure/telegram"
)

func main() {
	ctx := context.Background()

	client, err := telegram.NewClient(telegram.DefaultClientConfig(os.Getenv("TELEGRAM_BOT_TOKEN")))
	if err != nil {
		log.Fatal(err)
	}

	var chatID int64 = 123456789

	_, err = client.SendMessage(ctx, telegram.SendMessageRequest{
		ChatID:      chatID,
		Text:        "<b>Готово!</b>",
		ParseMode:   telegram.ParseModeHTML,
		ReplyMarkup: telegram.InlineKeyboard(telegram.Row(telegram.CallbackButton("Открыть", "open:42"))),
	})
	switch {
	case telegram.IsCode(err, 403): // пользователь заблокировал бота
		log.Println("blocked")
	case err != nil:
		log.Fatal(err)
	}

	// Метод без обёртки: payload — JSON, результат декодируется в out (здесь — telegram.Message).
	var dice telegram.Message
	err = client.Call(ctx, "sendDice", chatID, map[string]any{"chat_id": chatID, "emoji": "🎲"}, &dice)
	if err != nil {
		log.Fatal(err)
	}
}
```

Обёртки: `SendMessage`, `SendPhoto`, `EditMessageText`, `EditMessageReplyMarkup`, `DeleteMessage`,
`AnswerCallbackQuery`, `SendChatAction`, `GetMe`, `GetUpdates`, `SetWebhook`, `DeleteWebhook`,
`SetMyCommands`, `Healthcheck`. Клавиатуры: `InlineKeyboard`, `Row`, `CallbackButton`, `URLButton`,
`WebAppButton`; обычная клавиатура — `ReplyKeyboardMarkup`, убрать — `ReplyKeyboardRemove`.

### Конфигурация

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | — | **обязательна** |
| `TELEGRAM_API_URL` | `https://api.telegram.org` | свой Bot API server или заглушка в тестах |
| `TELEGRAM_REQUEST_TIMEOUT` | `15s` | таймаут обычного вызова (`getUpdates` считает свой) |
| `TELEGRAM_GLOBAL_RATE` | `30` | исходящих вызовов в секунду на бота |
| `TELEGRAM_CHAT_INTERVAL` | `1s` | интервал между сообщениями в один личный чат |
| `TELEGRAM_GROUP_INTERVAL` | `3s` | то же для групп и каналов (Telegram: 20 в минуту) |
| `TELEGRAM_CHAT_BURST` | `3` | сколько сообщений в чат можно отправить подряд без ожидания |
| `TELEGRAM_MAX_RETRIES` | `3` | повторов после 429 Too Many Requests |

`telegram.DefaultClientConfig(token)` — те же значения для сборки в коде. `telegram.WithHTTPClient(c)` — свой `*http.Client`.

### Метрики, трейсы, логи

| Метрика | Метки | |
|---|---|---|
| `telegram.api.request.count` | `method`, `status` (`ok`, `error` или код Bot API) | вызовы |
| `telegram.api.request.duration` | `method`, `status` | с; для `getUpdates` включает ожидание long polling |
| `telegram.api.rate_limited.count` | `method` | ответы 429 |
| `telegram.api.throttle.duration` | `method` | ожидание локального лимитера, с (пишется при ожидании ≥ 1 мс) |

Трейсы: спан `telegram.<method>` с атрибутами `telegram.method` и `telegram.chat_id`. Логи: предупреждение
при 429 с паузой.

### Связь с другими частями и пакетами

- [бот](#бот) создаёт клиент из `telegram.Config.API` сам; доступ — `b.API()` / `c.API()`.
- Health: `client.Healthcheck` (`getMe`) подходит для `health.Registry.Register`.
- Метрики и трейсы — в глобальные провайдеры ([запуск сервиса](#запуск-сервиса) или `observability/*`).

### Подводные камни

- Ошибки Bot API — `*telegram.APIError` с `Code`, `Description`, `RetryAfter`; различайте их через
  `telegram.IsCode(err, code)` / `errors.As`, а не по тексту. Правка без изменений — `telegram.IsMessageNotModified(err)`.
- `Call(ctx, method, chatID, payload, out)`: ненулевой `chatID` включает лимит на чат (отрицательный —
  группа/канал с `TELEGRAM_GROUP_INTERVAL`); передавайте его для всех методов, отправляющих или
  меняющих сообщения. Глобальный лимит действует на все методы, кроме `getUpdates`.
- `payload` уходит JSON-ом: загрузка файлов (multipart) не поддерживается — передавайте `file_id` или URL.
- Лимитер локальный для процесса: несколько копий сервиса с одним токеном делят лимит Telegram, но
  не знают друг о друге — уменьшайте `TELEGRAM_GLOBAL_RATE` пропорционально.

## Хранилище шагов диалога

Хранилище шагов диалога Telegram-бота: имя шага (`"awaiting_title"`) плюс произвольные данные в JSON,
отдельно для каждого пользователя в каждом чате, со сроком жизни. Три реализации интерфейса `Store`:
в памяти (тесты, локальная разработка), PostgreSQL и Redis (переживают рестарт и работают при
нескольких копиях бота). Обычно используется через `telegram.WithStateStore` и `telegram.Context.SetState`.

### Пример

```go
package main

import (
	"context"
	"log"

	"github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/arrowwhi/infrastructure/connections/redis"
	"github.com/arrowwhi/infrastructure/telegram"
)

func newStore(ctx context.Context, kind string, pool *postgres.Pool, rdb *redis.Client) (telegram.Store, error) {
	switch kind {
	case "postgres":
		store, err := telegram.NewPostgresStore(pool, "") // таблица telegram_states
		if err != nil {
			return nil, err
		}
		return store, store.EnsureSchema(ctx) // в сервисе лучше миграцией: telegram.PostgresStateSchema("")
	case "redis":
		return telegram.NewRedisStore(rdb.Client, ""), nil // ключи telegram:state:<chat>:<user>
	default:
		return telegram.NewMemoryStore(), nil
	}
}

func main() {
	ctx := context.Background()

	store, err := newStore(ctx, "memory", nil, nil)
	if err != nil {
		log.Fatal(err)
	}

	b, err := telegram.New(telegram.DefaultConfig("123:abc"), telegram.WithStateStore(store))
	if err != nil {
		log.Fatal(err)
	}
	_ = b

	// Напрямую (например, из воркера):
	key := telegram.StateKey{ChatID: 42, UserID: 42}
	s, err := telegram.NewState("awaiting_title", map[string]string{"source": "worker"})
	if err != nil {
		log.Fatal(err)
	}
	if err = store.Set(ctx, key, s, 0); err != nil { // ttl <= 0 — без срока жизни
		log.Fatal(err)
	}
}
```

В обработчиках бота: `c.SetState(name, data)`, `c.State()` → `(telegram.State, ok, err)`,
`s.Decode(&v)`, `c.ClearState()`.

### Конфигурация

Переменных нет. Параметры конструкторов:

| Конструктор | Параметр | По умолчанию | |
|---|---|---|---|
| `telegram.NewPostgresStore(pool, table)` | `table` | `telegram_states` (`telegram.DefaultPostgresTable`) | |
| `telegram.NewRedisStore(client, prefix)` | `prefix` | `telegram:state:` (`telegram.DefaultRedisStatePrefix`) | любой `goredis.UniversalClient` |
| `telegram.NewMemoryStore()` | — | — | |

Срок жизни задаёт бот: `TELEGRAM_STATE_TTL` (24h), см. [бот](#бот).

### Метрики, трейсы, логи

Своих нет. Запросы `NewPostgresStore` идут через `connections/postgres`, `NewRedisStore` — через переданный
клиент, поэтому попадают в их метрики и трейсы (и в трейс обновления бота).

### Связь с другими частями и пакетами

- [бот](#бот): `telegram.WithStateStore(store)`; без опции — `NewMemoryStore`.
- [`connections/postgres`](../connections/postgres/README.md): `NewPostgresStore` принимает `*postgres.Pool`;
  DDL — `telegram.PostgresStateSchema(table)` для своей миграции ([`migrate`](../connections/postgres/migrate/README.md))
  или `store.EnsureSchema(ctx)`.
- [`connections/redis`](../connections/redis/README.md): передавайте встроенный клиент — `rdb.Client`.

### Подводные камни

- `MemoryStore` теряет состояния при рестарте и не разделяет их между копиями бота — только для тестов
  и локальной разработки.
- PostgreSQL не удаляет истёкшие строки сам: `Get` их не возвращает, но место освобождает только
  `store.Cleanup(ctx)` — запускайте периодически (например, раз в час). В Redis истечение — по TTL ключа.
- `Get` для отсутствующего или истёкшего состояния возвращает `telegram.ErrStateNotFound`
  (`c.State()` превращает его в `ok == false`).
- Данные хранятся как JSON: меняя структуру данных шага, помните о состояниях, сохранённых старой
  версией бота (до `TELEGRAM_STATE_TTL`).
