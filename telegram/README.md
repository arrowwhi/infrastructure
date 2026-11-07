# Telegram

Скелет Telegram-бота: всё, что повторяется в каждом боте, уже собрано, остаётся
написать обработчики.

| Пакет | Что делает |
|---|---|
| `telegram/app` | Запуск сервиса: конфиг из env, логгер, метрики, трейсинг, health-проверки, служебный порт, остановка в обратном порядке |
| `telegram/api` | Рукописный клиент Bot API: лимиты Telegram, повтор после 429, метрики и трейсы, токен не попадает в ошибки |
| `telegram/bot` | Роутер команд/кнопок/текста, шаги диалога, воркеры с порядком внутри чата, polling и webhook |
| `telegram/state` | Хранилище шагов диалога: в памяти, PostgreSQL или Redis |

Рабочий пример — [`examples/tgbot`](../examples/tgbot/main.go).

## Минимальный бот

```go
type Config struct {
    App      app.Config
    Telegram bot.Config
}

func main() {
    ctx := context.Background()

    var cfg Config
    if err := app.LoadConfig(&cfg); err != nil { log.Fatal(err) }

    a, err := app.New(ctx, cfg.App)
    if err != nil { log.Fatal(err) }

    b, err := bot.New(cfg.Telegram)
    if err != nil { log.Fatal(err) }

    b.Command("start", "Начать", func(c *bot.Context) error {
        _, err := c.Reply("Привет!")
        return err
    })

    a.Health().Register("telegram", b.Healthcheck)

    if err := a.Run(ctx, b); err != nil { log.Fatal(err) }
}
```

```bash
SERVICE_NAME=my-bot TELEGRAM_BOT_TOKEN=123:abc go run .
```

## telegram/app

`app.New` инициализирует логгер, трейсинг и метрики (глобально — все коннекторы
библиотеки подхватывают их сами) и реестр health-проверок. `app.Run` поднимает
служебный порт, запускает компоненты (`Run(ctx) error`) и ждёт SIGINT/SIGTERM или
ошибки любого компонента. Остановка: компоненты → функции `OnShutdown` в обратном
порядке регистрации → сброс телеметрии.

Служебный порт (`ADMIN_PORT`, по умолчанию 9090):

- `/healthz` — процесс жив (зависимости не проверяет — иначе падение базы перезапустит все поды);
- `/ready` — результат health-проверок, 503 при падении критичной;
- `/metrics` — при `METRICS_EXPORTER=prometheus`;
- `/debug/pprof/*` — при `ADMIN_PPROF_ENABLED=true`.

Основные переменные:

| Переменная | По умолчанию | |
|---|---|---|
| `SERVICE_NAME` | — | обязательна |
| `SERVICE_VERSION`, `ENVIRONMENT` | `dev`, `local` | попадают в логи, метрики, трейсы |
| `LOG_LEVEL`, `LOG_PRETTYOUTPUT` | `info`, `false` | |
| `METRICS_EXPORTER` | `prometheus` | или `otlp-grpc`, `otlp-http` |
| `OTEL_ENABLED`, `OTEL_EXPORTER`, `OTEL_EXPORTER_OTLP_ENDPOINT` | `false`, `otlp-grpc`, `localhost:4317` | трейсинг |
| `ADMIN_PORT`, `ADMIN_PPROF_ENABLED` | `9090`, `false` | |
| `SHUTDOWN_TIMEOUT` | `30s` | |

`app.LoadConfig` читает `.env` из рабочего каталога, если он есть; переменные окружения
его перекрывают.

## telegram/bot

### Маршруты

```go
b.Command("rate", "Оценить фильм", h) // /rate, /rate@my_bot, /rate аргументы → c.Args()
b.Callback("score", h)                // кнопки с data "score" или "score:..." → c.CallbackArgs()
b.OnState("awaiting_title", h)        // сообщения, пока диалог в этом шаге
b.Text(h)                             // остальной текст
b.Default(h)                          // всё, что не подошло (иначе — метрика unhandled)
b.Use(middleware...)                  // свои middleware (авторизация, rate limit пользователя…)
```

Порядок для сообщений: команда → шаг диалога → текст → default. Команды срабатывают
в любом шаге диалога, поэтому `/start` всегда выводит пользователя из диалога.
Команды с описанием публикуются в меню бота при старте (`TELEGRAM_SET_COMMANDS`).

Данные кнопок собирайте через `bot.CallbackData(prefix, args...)` — он проверяет
лимит Telegram в 64 байта.

### Шаги диалога

```go
b.Command("rate", "Оценить фильм", func(c *bot.Context) error {
    if err := c.SetState("awaiting_title", nil); err != nil { return err }
    _, err := c.Reply("Какой фильм?")
    return err
})

b.OnState("awaiting_title", func(c *bot.Context) error {
    return c.SetState("awaiting_score", movie{Title: c.Text()}) // данные переживают рестарт
})

b.Callback("score", func(c *bot.Context) error {
    s, ok, err := c.State()
    // ... s.Decode(&m), c.ClearState()
})
```

Состояние хранится отдельно для каждого пользователя в каждом чате и истекает через
`TELEGRAM_STATE_TTL` (24 часа). Хранилище задаётся опцией `bot.WithStateStore`:

```go
state.NewMemory()                      // тесты и локальная разработка
state.NewPostgres(pool, "")            // таблица telegram_states; DDL — state.PostgresSchema
state.NewRedis(rdb.Client, "")         // ключи telegram:state:*, TTL средствами Redis
```

### Ошибки

Ошибка обработчика и паника логируются (с `trace_id`), попадают в метрики и трейс
и передаются в `ErrorHandler`. Готовый вариант: `bot.WithErrorHandler(bot.ReplyOnError("Что-то пошло не так"))`.
На нажатие кнопки бот всегда отвечает (`answerCallbackQuery`), даже если обработчик
забыл — иначе на кнопке крутятся «часики».

### Параллельность

`TELEGRAM_WORKERS` воркеров; обновления одного чата всегда попадают к одному воркеру
и обрабатываются строго по порядку, разные чаты — параллельно. При остановке приём
прекращается, а начатые обработки завершаются (не дольше `TELEGRAM_HANDLER_TIMEOUT`).

### Polling и webhook

| | `TELEGRAM_MODE=polling` | `TELEGRAM_MODE=webhook` |
|---|---|---|
| Где | локальная разработка | продакшен |
| Публичный адрес | не нужен | `TELEGRAM_WEBHOOK_URL` (HTTPS) |
| Безопасность | — | `TELEGRAM_WEBHOOK_SECRET` обязателен, без него 401 |
| Сервер | — | встроенный на `TELEGRAM_WEBHOOK_PORT` (8443); пустой порт — смонтируйте `b.WebhookHandler()` на свой роутер |

Polling переживает rolling-деплой: пока старый экземпляр держит `getUpdates`
(409 Conflict), новый ждёт без шума в логах.

## telegram/api

Клиент можно использовать и отдельно — например, для рассылок из воркера:

```go
client, _ := api.New(api.DefaultConfig(token))
_, err := client.SendMessage(ctx, api.SendMessageRequest{
    ChatID:      chatID,
    Text:        "Готово!",
    ReplyMarkup: api.InlineKeyboard(api.Row(api.CallbackButton("Открыть", "open:42"))),
})
```

- Лимиты Telegram соблюдаются сами: ~30 сообщений/с на бота (`TELEGRAM_GLOBAL_RATE`),
  1 сообщение/с в личный чат и 20/мин в группу (`TELEGRAM_CHAT_INTERVAL`, `TELEGRAM_GROUP_INTERVAL`).
- На 429 клиент ждёт `retry_after` и повторяет (`TELEGRAM_MAX_RETRIES`).
- Ошибки Bot API — `*api.Error` с кодом: `api.IsCode(err, 403)` (бот заблокирован),
  `api.IsMessageNotModified(err)`.
- Методов без обёртки нет проблем вызвать: `client.Call(ctx, "sendDocument", chatID, payload, &out)`.

## Метрики

| Метрика | Метки | |
|---|---|---|
| `telegram.bot.updates.count` | `type`, `route`, `status` (ok, error, panic, unhandled) | обновления |
| `telegram.bot.update.duration` | `type`, `route` | время обработки, с |
| `telegram.bot.queue.size` | | ждут обработки; растёт — обработчики не успевают |
| `telegram.api.request.count` | `method`, `status` (ok или код ошибки) | вызовы Bot API |
| `telegram.api.request.duration` | `method`, `status` | с (getUpdates включает long polling) |
| `telegram.api.rate_limited.count` | `method` | ответы 429 |
| `telegram.api.throttle.duration` | `method` | ожидание локального лимитера, с |
| `health.check.status` | `check`, `critical` | 1/0 по каждой проверке |

Трейсы: спан `telegram.update <route>` на каждое обновление и дочерние спаны
`telegram.<method>` на вызовы Bot API; запросы к базе и Redis из обработчика
попадают в тот же трейс.
