# telegram/api

Небольшой рукописный клиент Telegram Bot API без собственного диспетчера (роутинг — в
[`telegram/bot`](../bot/README.md)). Клиент сам соблюдает лимиты Telegram (глобальный и на чат),
повторяет запрос после 429 с паузой из `retry_after`, не пишет токен бота в ошибки и снимает метрики
и трейсы каждого вызова. Подходит и отдельно от бота — например, для рассылок из воркера.

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import "github.com/arrowwhi/infrastructure/telegram/api"
```

## Пример

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/arrowwhi/infrastructure/telegram/api"
)

func main() {
	ctx := context.Background()

	client, err := api.New(api.DefaultConfig(os.Getenv("TELEGRAM_BOT_TOKEN")))
	if err != nil {
		log.Fatal(err)
	}

	var chatID int64 = 123456789

	_, err = client.SendMessage(ctx, api.SendMessageRequest{
		ChatID:      chatID,
		Text:        "<b>Готово!</b>",
		ParseMode:   api.ParseModeHTML,
		ReplyMarkup: api.InlineKeyboard(api.Row(api.CallbackButton("Открыть", "open:42"))),
	})
	switch {
	case api.IsCode(err, 403): // пользователь заблокировал бота
		log.Println("blocked")
	case err != nil:
		log.Fatal(err)
	}

	// Метод без обёртки: payload — JSON, результат декодируется в out (здесь — api.Message).
	var dice api.Message
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

## Конфигурация

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

`api.DefaultConfig(token)` — те же значения для сборки в коде. `api.WithHTTPClient(c)` — свой `*http.Client`.

## Метрики, трейсы, логи

| Метрика | Метки | |
|---|---|---|
| `telegram.api.request.count` | `method`, `status` (`ok`, `error` или код Bot API) | вызовы |
| `telegram.api.request.duration` | `method`, `status` | с; для `getUpdates` включает ожидание long polling |
| `telegram.api.rate_limited.count` | `method` | ответы 429 |
| `telegram.api.throttle.duration` | `method` | ожидание локального лимитера, с (пишется при ожидании ≥ 1 мс) |

Трейсы: спан `telegram.<method>` с атрибутами `telegram.method` и `telegram.chat_id`. Логи: предупреждение
при 429 с паузой.

## Связь с другими пакетами

- [`telegram/bot`](../bot/README.md) создаёт клиент из `bot.Config.API` сам; доступ — `b.API()` / `c.API()`.
- Health: `client.Healthcheck` (`getMe`) подходит для `health.Registry.Register`.
- Метрики и трейсы — в глобальные провайдеры (`telegram/app` или `observability/*`).

## Подводные камни

- Ошибки Bot API — `*api.Error` с `Code`, `Description`, `RetryAfter`; различайте их через
  `api.IsCode(err, code)` / `errors.As`, а не по тексту. Правка без изменений — `api.IsMessageNotModified(err)`.
- `Call(ctx, method, chatID, payload, out)`: ненулевой `chatID` включает лимит на чат (отрицательный —
  группа/канал с `TELEGRAM_GROUP_INTERVAL`); передавайте его для всех методов, отправляющих или
  меняющих сообщения. Глобальный лимит действует на все методы, кроме `getUpdates`.
- `payload` уходит JSON-ом: загрузка файлов (multipart) не поддерживается — передавайте `file_id` или URL.
- Лимитер локальный для процесса: несколько копий сервиса с одним токеном делят лимит Telegram, но
  не знают друг о друге — уменьшайте `TELEGRAM_GLOBAL_RATE` пропорционально.
