# Telegram

Скелет Telegram-бота: всё, что повторяется в каждом боте (запуск, конфиг, лимиты Telegram, порядок
обработки, шаги диалога, метрики и трейсы), уже собрано — остаётся написать обработчики.

| Пакет | Что делает |
|---|---|
| [`telegram/app`](app/README.md) | Запуск сервиса: конфиг из env, логгер, метрики, трейсинг, health-проверки, служебный порт, остановка в обратном порядке |
| [`telegram/bot`](bot/README.md) | Роутер команд/кнопок/текста, шаги диалога, воркеры с порядком внутри чата, polling и webhook |
| [`telegram/api`](api/README.md) | Рукописный клиент Bot API: лимиты Telegram, повтор после 429, метрики и трейсы, токен не попадает в ошибки |
| [`telegram/state`](state/README.md) | Хранилище шагов диалога: в памяти, PostgreSQL или Redis |

Как они связаны: `app` поднимает инфраструктуру и запускает `bot` как компонент; `bot` получает
обновления через `api` и хранит шаги диалога в `state`; `api` можно использовать и без бота
(рассылки из воркера). `app` не привязан к Telegram — им удобно запускать любой сервис.

## Минимальный бот

```go
package main

import (
	"context"
	"log"

	"github.com/arrowwhi/infrastructure/telegram/app"
	"github.com/arrowwhi/infrastructure/telegram/bot"
)

type Config struct {
	App      app.Config
	Telegram bot.Config
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

	b, err := bot.New(cfg.Telegram)
	if err != nil {
		log.Fatal(err)
	}

	b.Command("start", "Начать", func(c *bot.Context) error {
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
