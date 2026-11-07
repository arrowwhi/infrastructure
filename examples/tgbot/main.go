// Пример Telegram-бота на telegram/app + telegram/bot.
//
// Запуск локально (long polling, состояние в памяти):
//
//	SERVICE_NAME=example-bot TELEGRAM_BOT_TOKEN=123:abc go run ./examples/tgbot
//
// Метрики и пробы — на служебном порту: http://localhost:9090/metrics, /healthz, /ready.
// Хранилище состояний выбирается переменной STATE_STORE: memory, redis или postgres.
package main

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/arrowwhi/infrastructure/connections/redis"
	"github.com/arrowwhi/infrastructure/telegram/api"
	"github.com/arrowwhi/infrastructure/telegram/app"
	"github.com/arrowwhi/infrastructure/telegram/bot"
	"github.com/arrowwhi/infrastructure/telegram/state"
)

// Config — вся конфигурация бота, читается из env одним вызовом app.LoadConfig.
type Config struct {
	Postgres   postgres.Config
	Redis      redis.Config
	StateStore string `env:"STATE_STORE" env-default:"memory"`
	Telegram   bot.Config
	App        app.Config
}

// movie — данные, которые диалог «оценить фильм» переносит между шагами.
type movie struct {
	Title string `json:"title"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()

	var cfg Config
	if err := app.LoadConfig(&cfg); err != nil {
		return err
	}

	a, err := app.New(ctx, cfg.App)
	if err != nil {
		return err
	}

	store, err := newStateStore(ctx, cfg, a)
	if err != nil {
		return err
	}

	b, err := bot.New(cfg.Telegram,
		bot.WithStateStore(store),
		bot.WithErrorHandler(bot.ReplyOnError("Что-то пошло не так, попробуйте ещё раз.")),
	)
	if err != nil {
		return err
	}

	a.Health().Register("telegram", b.Healthcheck)

	registerHandlers(b)

	return a.Run(ctx, b)
}

func registerHandlers(b *bot.Bot) {
	b.Command("start", "Начать", func(c *bot.Context) error {
		_, err := c.Reply("Привет! Напиши /rate, чтобы оценить фильм.")

		return err
	})

	// Шаг 1: спрашиваем название.
	b.Command("rate", "Оценить фильм", func(c *bot.Context) error {
		if err := c.SetState("awaiting_title", nil); err != nil {
			return err
		}

		_, err := c.Reply("Какой фильм?")

		return err
	})

	// Шаг 2: запоминаем название и показываем кнопки с оценками.
	b.OnState("awaiting_title", func(c *bot.Context) error {
		if err := c.SetState("awaiting_score", movie{Title: c.Text()}); err != nil {
			return err
		}

		row := make([]api.InlineKeyboardButton, 0, 5)
		for score := 6; score <= 10; score++ {
			row = append(row, api.CallbackButton(strconv.Itoa(score), bot.MustCallbackData("score", strconv.Itoa(score))))
		}

		_, err := c.Reply("Оценка?", bot.WithKeyboard(api.InlineKeyboard(row)))

		return err
	})

	// Шаг 3: нажатие кнопки — достаём название из состояния и завершаем диалог.
	b.Callback("score", func(c *bot.Context) error {
		s, ok, err := c.State()
		if err != nil {
			return err
		}

		if !ok || s.Name != "awaiting_score" {
			return c.Answer("Этот диалог уже завершён", false)
		}

		var m movie
		if err = s.Decode(&m); err != nil {
			return err
		}

		if err = c.ClearState(); err != nil {
			return err
		}

		return c.Edit(fmt.Sprintf("«%s» — %s/10. Сохранено!", m.Title, c.CallbackArgs()[0]), nil, "")
	})

	b.Text(func(c *bot.Context) error {
		_, err := c.Reply("Не понимаю. Попробуйте /rate.")

		return err
	})
}

// newStateStore выбирает хранилище состояний по STATE_STORE.
func newStateStore(ctx context.Context, cfg Config, a *app.App) (state.Store, error) {
	switch cfg.StateStore {
	case "memory":
		return state.NewMemory(), nil

	case "redis":
		rdb, err := redis.New(ctx, cfg.Redis)
		if err != nil {
			return nil, err
		}

		a.Health().Register("redis", rdb.Healthcheck)
		a.OnShutdown(func(context.Context) error { return rdb.Close() })

		return state.NewRedis(rdb.Client, ""), nil

	case "postgres":
		pool, err := postgres.New(ctx, cfg.Postgres)
		if err != nil {
			return nil, err
		}

		a.Health().Register("postgres", pool.Healthcheck)
		a.OnShutdown(func(context.Context) error { pool.Close(); return nil })

		store, err := state.NewPostgres(pool, "")
		if err != nil {
			return nil, err
		}

		// В реальном сервисе таблицу лучше создавать миграцией: state.PostgresSchema(table).
		return store, store.EnsureSchema(ctx)

	default:
		return nil, fmt.Errorf("unknown STATE_STORE %q (memory, redis, postgres)", cfg.StateStore)
	}
}
