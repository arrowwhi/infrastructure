// Package bot is a Telegram bot skeleton: routing, dialog state, ordered concurrent
// processing, polling and webhook transports, and built-in logging, metrics, tracing
// and panic recovery.
//
//	b, err := bot.New(cfg.Telegram, bot.WithStateStore(store))
//	b.Command("start", "Начать", func(c *bot.Context) error {
//	    _, err := c.Reply("Привет!")
//	    return err
//	})
//	b.Callback("rate", rateHandler)
//	b.OnState("awaiting_review", reviewHandler)
//	b.Text(searchHandler)
//
//	a.Health().Register("telegram", b.Healthcheck)
//	a.Run(ctx, b) // telegram/app
package bot

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/arrowwhi/infrastructure/telegram/api"
	"github.com/arrowwhi/infrastructure/telegram/state"
)

const instrumentationName = "github.com/arrowwhi/infrastructure/telegram/bot"

// ErrorHandler вызывается, когда обработчик вернул ошибку или запаниковал.
// Типичное применение — ответить пользователю «что-то пошло не так».
type ErrorHandler func(c *Context, err error)

// Bot — Telegram-бот. Маршруты регистрируются до Run.
type Bot struct {
	api          *api.Client
	store        state.Store
	router       *router
	errorHandler ErrorHandler
	metrics      *botMetrics
	tracer       trace.Tracer
	dispatcher   *dispatcher
	username     string
	middlewares  []Middleware
	cfg          Config
}

// Option настраивает бота.
type Option func(*Bot)

// WithStateStore задаёт хранилище состояний диалога (по умолчанию — в памяти процесса).
func WithStateStore(store state.Store) Option {
	return func(b *Bot) { b.store = store }
}

// WithAPIClient подставляет готовый клиент Bot API (тесты, общий клиент на несколько ботов).
func WithAPIClient(c *api.Client) Option {
	return func(b *Bot) { b.api = c }
}

// WithErrorHandler задаёт реакцию на ошибки обработчиков.
func WithErrorHandler(h ErrorHandler) Option {
	return func(b *Bot) { b.errorHandler = h }
}

// ReplyOnError — готовый ErrorHandler: отвечает пользователю text (ошибка уже залогирована).
func ReplyOnError(text string) ErrorHandler {
	return func(c *Context, _ error) {
		if c.Callback() != nil {
			_ = c.Answer(text, false)

			return
		}

		if c.ChatID() != 0 {
			_, _ = c.Reply(text)
		}
	}
}

// New создаёт бота.
func New(cfg Config, opts ...Option) (*Bot, error) {
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}

	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1
	}

	b := &Bot{cfg: cfg, router: newRouter(), tracer: otel.Tracer(instrumentationName)}

	for _, opt := range opts {
		opt(b)
	}

	if b.api == nil {
		client, err := api.New(cfg.API)
		if err != nil {
			return nil, err
		}

		b.api = client
	}

	if b.store == nil {
		b.store = state.NewMemory()
	}

	m, err := newBotMetrics(otel.Meter(instrumentationName))
	if err != nil {
		return nil, err
	}

	b.metrics = m
	b.dispatcher = newDispatcher(cfg.Workers, cfg.QueueSize, b.process)

	if err = b.metrics.observeQueue(b.dispatcher); err != nil {
		return nil, err
	}

	return b, nil
}

// API возвращает клиент Bot API — для рассылок и уведомлений вне обработчиков.
func (b *Bot) API() *api.Client { return b.api }

// Store возвращает хранилище состояний.
func (b *Bot) Store() state.Store { return b.store }

// Username возвращает @username бота (известен после Run).
func (b *Bot) Username() string { return b.username }

// Healthcheck — проверка для observability/health (getMe).
func (b *Bot) Healthcheck(ctx context.Context) error {
	return b.api.Healthcheck(ctx)
}

// Run получает обновления в режиме Config.Mode и обрабатывает их, пока не отменён ctx.
// Реализует telegram/app.Component. При остановке новые обновления перестают
// приниматься, а начатые обрабатываются до конца (не дольше HandlerTimeout).
func (b *Bot) Run(ctx context.Context) error {
	if err := b.setup(ctx); err != nil {
		return err
	}

	b.dispatcher.start()

	var err error

	switch b.cfg.Mode {
	case ModeWebhook:
		err = b.runWebhook(ctx)
	case ModePolling, "":
		err = b.runPolling(ctx)
	default:
		err = fmt.Errorf("bot: unknown mode %q", b.cfg.Mode)
	}

	b.dispatcher.stop()
	logger.Info(ctx).Msg("Telegram bot stopped")

	return err
}

func (b *Bot) setup(ctx context.Context) error {
	me, err := b.api.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("bot: getMe: %w", err)
	}

	b.username = me.Username

	if commands := b.router.menuCommands(); b.cfg.SetCommands && len(commands) > 0 {
		if err = b.api.SetMyCommands(ctx, commands); err != nil {
			// Меню — косметика: бот работает и без него.
			logger.Warn(ctx).Err(err).Msg("Failed to set bot commands")
		}
	}

	logger.Info(ctx).Str("username", me.Username).Str("mode", b.cfg.Mode).Msg("Telegram bot started")

	return nil
}

// Enqueue ставит обновление в очередь обработки (используют транспорты; полезно в тестах).
// Блокируется, если очередь воркера заполнена.
func (b *Bot) Enqueue(ctx context.Context, u api.Update) error {
	return b.dispatcher.enqueue(ctx, u)
}

// HandleUpdate синхронно обрабатывает одно обновление со всеми middleware.
// Транспорты вызывают его из воркеров; в тестах можно вызывать напрямую.
func (b *Bot) HandleUpdate(ctx context.Context, u api.Update) {
	ctx, cancel := context.WithTimeout(ctx, b.cfg.HandlerTimeout)
	defer cancel()

	c := &Context{Context: ctx, bot: b, Update: u}

	start := time.Now()
	status := "ok"

	ctx, span := b.tracer.Start(ctx, "telegram.update",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.Int64("telegram.update_id", u.UpdateID),
			attribute.String("telegram.update_type", u.Type()),
			attribute.Int64("telegram.chat_id", c.ChatID()),
			attribute.Int64("telegram.user_id", c.UserID()),
		),
	)
	c.Context = ctx

	defer func() {
		if p := recover(); p != nil {
			status = "panic"
			err := fmt.Errorf("panic: %v", p)

			logger.Error(ctx).
				Interface("panic", p).
				Bytes("stack", debug.Stack()).
				Int64("chat_id", c.ChatID()).
				Str("route", c.route).
				Msg("Telegram handler panic recovered")

			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			b.reportError(c, err)
		}

		_ = c.Answer("", false)

		span.SetName("telegram.update " + c.route)
		span.SetAttributes(attribute.String("telegram.route", c.route))
		span.End()

		b.metrics.recordUpdate(ctx, u.Type(), c.route, status, time.Since(start))

		logger.Debug(ctx).
			Int64("update_id", u.UpdateID).
			Int64("chat_id", c.ChatID()).
			Int64("user_id", c.UserID()).
			Str("route", c.route).
			Str("status", status).
			Dur("duration", time.Since(start)).
			Msg("Telegram update handled")
	}()

	handler, err := b.resolve(c)
	if err != nil {
		status = "error"
		b.fail(c, span, err)

		return
	}

	if handler == nil {
		status = routeUnhandled

		return
	}

	for i := len(b.middlewares) - 1; i >= 0; i-- {
		handler = b.middlewares[i](handler)
	}

	if err = handler(c); err != nil {
		status = "error"
		b.fail(c, span, err)
	}
}

func (b *Bot) fail(c *Context, span trace.Span, err error) {
	logger.Error(c).
		Err(err).
		Int64("update_id", c.Update.UpdateID).
		Int64("chat_id", c.ChatID()).
		Int64("user_id", c.UserID()).
		Str("route", c.route).
		Msg("Telegram handler failed")

	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	b.reportError(c, err)
}

func (b *Bot) reportError(c *Context, err error) {
	if b.errorHandler == nil || errors.Is(err, context.Canceled) {
		return
	}

	b.errorHandler(c, err)
}

// process — точка входа воркера.
func (b *Bot) process(u api.Update) {
	// Обработка не отменяется вместе с приёмом обновлений: начатый ответ
	// пользователю нужно довести до конца (ограничено HandlerTimeout).
	b.HandleUpdate(context.Background(), u)
}
