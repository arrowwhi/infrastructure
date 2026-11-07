# telegram/bot

Каркас Telegram-бота: роутер команд, кнопок и текста, шаги диалога с хранением в
[`telegram/state`](../state/README.md), параллельные воркеры со строгим порядком внутри чата,
получение обновлений через long polling или webhook, встроенные recovery, логи, метрики и трейсы
на каждое обновление. Бот реализует `app.Component` и запускается через [`telegram/app`](../app/README.md).

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import "github.com/arrowwhi/infrastructure/telegram/bot"
```

## Пример

```go
package main

import (
	"context"
	"log"

	"github.com/arrowwhi/infrastructure/telegram/api"
	"github.com/arrowwhi/infrastructure/telegram/app"
	"github.com/arrowwhi/infrastructure/telegram/bot"
)

type Config struct {
	App      app.Config
	Telegram bot.Config
}

type movie struct {
	Title string `json:"title"`
}

func main() {
	ctx := context.Background()

	var cfg Config
	if err := app.LoadConfig(&cfg); err != nil {
		log.Fatal(err)
	}

	a, err := app.New(ctx, cfg.App)
	if err != nil {
		log.Fatal(err)
	}

	b, err := bot.New(cfg.Telegram, bot.WithErrorHandler(bot.ReplyOnError("Что-то пошло не так")))
	if err != nil {
		log.Fatal(err)
	}

	b.Command("start", "Начать", func(c *bot.Context) error {
		_, err := c.Reply("Привет! /rate — оценить фильм")
		return err
	})

	b.Command("rate", "Оценить фильм", func(c *bot.Context) error {
		if err := c.SetState("awaiting_title", nil); err != nil {
			return err
		}
		_, err := c.Reply("Какой фильм?")
		return err
	})

	b.OnState("awaiting_title", func(c *bot.Context) error {
		if err := c.SetState("awaiting_score", movie{Title: c.Text()}); err != nil { // данные переживают рестарт
			return err
		}
		kb := api.InlineKeyboard(api.Row(
			api.CallbackButton("👍", bot.MustCallbackData("score", "10")),
			api.CallbackButton("👎", bot.MustCallbackData("score", "1")),
		))
		_, err := c.Reply("Оценка?", bot.WithKeyboard(kb))
		return err
	})

	b.Callback("score", func(c *bot.Context) error {
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

Полный пример с выбором хранилища состояний — [`examples/tgbot`](../../examples/tgbot/main.go).

### Маршруты

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

Данные кнопок собирайте через `bot.CallbackData(prefix, args...)` (ошибка при превышении 64 байт)
или `bot.MustCallbackData` (паника — для констант).

### Шаги диалога

`c.SetState(name, data)` сохраняет шаг и данные (JSON) для пары «пользователь + чат» на
`TELEGRAM_STATE_TTL`; `c.State()` читает, `c.ClearState()` завершает. Хранилище — опция
`bot.WithStateStore(store)`, по умолчанию в памяти процесса (см. [`telegram/state`](../state/README.md)).

### Ответы

`c.Reply(text, opts...)` с опциями `bot.WithKeyboard`, `bot.WithParseMode(api.ParseModeHTML)`,
`bot.Silent()`, `bot.WithoutPreview()`; `c.Edit(text, markup, parseMode)` — правка сообщения с кнопкой
(«message is not modified» не считается ошибкой); `c.Answer(text, alert)` — ответ на нажатие;
`c.API()` — полный клиент [`telegram/api`](../api/README.md).

## Конфигурация

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` и другие `TELEGRAM_*` клиента | — | см. [`telegram/api`](../api/README.md) |
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

## Метрики, трейсы, логи

| Метрика | Метки | |
|---|---|---|
| `telegram.bot.updates.count` | `type`, `route`, `status` (`ok`, `error`, `panic`, `unhandled`) | обновления |
| `telegram.bot.update.duration` | `type`, `route` | время обработки, с |
| `telegram.bot.queue.size` | | ждут обработки; растёт — обработчики не успевают |

Плюс метрики клиента `telegram.api.*` (см. [`telegram/api`](../api/README.md)).

Трейсы: спан `telegram.update <route>` на каждое обновление (атрибуты `telegram.update_id`,
`telegram.chat_id`, `telegram.user_id`, `telegram.route`), дочерние спаны `telegram.<method>` на
вызовы Bot API; запросы к базе и Redis из обработчика попадают в тот же трейс, если передавать им
`c` (он же `context.Context`). В webhook-режиме — ещё спан HTTP-запроса `telegram.webhook`.

Логи: ошибка обработчика и паника логируются с `trace_id`, попадают в метрики и трейс и передаются
в `ErrorHandler` (`bot.WithErrorHandler`; готовый — `bot.ReplyOnError(text)`).

## Связь с другими пакетами

- [`telegram/app`](../app/README.md): `a.Run(ctx, b)`, `a.Health().Register("telegram", b.Healthcheck)` (`getMe`).
- [`telegram/state`](../state/README.md): `bot.WithStateStore(state.NewPostgres(...) / state.NewRedis(...))`.
- [`telegram/api`](../api/README.md): `b.API()` — для рассылок вне обработчиков; `bot.WithAPIClient(c)` — свой клиент.
- [`http/server`](../../http/server/README.md): при `TELEGRAM_WEBHOOK_PORT=` — `srv.Router().Handle("/telegram/webhook", b.WebhookHandler())`.

## Подводные камни

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
