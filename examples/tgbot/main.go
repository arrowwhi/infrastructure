// Пример Telegram-бота на пакете telegram.
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
	"github.com/arrowwhi/infrastructure/telegram"
)

// Config — вся конфигурация бота, читается из env одним вызовом telegram.LoadConfig.
type Config struct {
	Postgres   postgres.Config
	Redis      redis.Config
	StateStore string `env:"STATE_STORE" env-default:"memory"`
	Telegram   telegram.Config
	App        telegram.AppConfig
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
	if err := telegram.LoadConfig(&cfg); err != nil {
		return err
	}

	a, err := telegram.NewApp(ctx, cfg.App)
	if err != nil {
		return err
	}

	store, err := newStateStore(ctx, cfg, a)
	if err != nil {
		return err
	}

	b, err := telegram.New(cfg.Telegram,
		telegram.WithStateStore(store),
		telegram.WithErrorHandler(telegram.ReplyOnError("Что-то пошло не так, попробуйте ещё раз.")),
	)
	if err != nil {
		return err
	}

	a.Health().Register("telegram", b.Healthcheck)

	registerHandlers(b)

	return a.Run(ctx, b)
}

func registerHandlers(b *telegram.Bot) {
	b.Command("start", "Начать", func(c *telegram.Context) error {
		_, err := c.Reply("Привет! Напиши /rate, чтобы оценить фильм.")

		return err
	})

	// Шаг 1: спрашиваем название.
	b.Command("rate", "Оценить фильм", func(c *telegram.Context) error {
		if err := c.SetState("awaiting_title", nil); err != nil {
			return err
		}

		_, err := c.Reply("Какой фильм?")

		return err
	})

	// Шаг 2: запоминаем название и показываем кнопки с оценками.
	b.OnState("awaiting_title", func(c *telegram.Context) error {
		if err := c.SetState("awaiting_score", movie{Title: c.Text()}); err != nil {
			return err
		}

		row := make([]telegram.InlineKeyboardButton, 0, 5)
		for score := 6; score <= 10; score++ {
			row = append(row, telegram.CallbackButton(strconv.Itoa(score), telegram.MustCallbackData("score", strconv.Itoa(score))))
		}

		_, err := c.Reply("Оценка?", telegram.WithKeyboard(telegram.InlineKeyboard(row)))

		return err
	})

	// Шаг 3: нажатие кнопки — достаём название из состояния и завершаем диалог.
	b.Callback("score", func(c *telegram.Context) error {
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

	b.Text(func(c *telegram.Context) error {
		_, err := c.Reply("Не понимаю. Попробуйте /rate.")

		return err
	})
}

// newStateStore выбирает хранилище состояний по STATE_STORE.
func newStateStore(ctx context.Context, cfg Config, a *telegram.App) (telegram.Store, error) {
	switch cfg.StateStore {
	case "memory":
		return telegram.NewMemoryStore(), nil

	case "redis":
		rdb, err := redis.New(ctx, cfg.Redis)
		if err != nil {
			return nil, err
		}

		a.Health().Register("redis", rdb.Healthcheck)
		a.OnShutdown(func(context.Context) error { return rdb.Close() })

		return telegram.NewRedisStore(rdb.Client, ""), nil

	case "postgres":
		pool, err := postgres.New(ctx, cfg.Postgres)
		if err != nil {
			return nil, err
		}

		a.Health().Register("postgres", pool.Healthcheck)
		a.OnShutdown(func(context.Context) error { pool.Close(); return nil })

		store, err := telegram.NewPostgresStore(pool, "")
		if err != nil {
			return nil, err
		}

		// В реальном сервисе таблицу лучше создавать миграцией: telegram.PostgresStateSchema(table).
		return store, store.EnsureSchema(ctx)

	default:
		return nil, fmt.Errorf("unknown STATE_STORE %q (memory, redis, postgres)", cfg.StateStore)
	}
}
