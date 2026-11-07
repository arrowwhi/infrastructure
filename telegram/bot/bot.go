// Package bot — прежний путь Telegram-бота.
//
// Deprecated: пакет оставлен для обратной совместимости, весь код переехал в
// [github.com/arrowwhi/infrastructure/telegram]. Все типы здесь — алиасы, поэтому
// обработчики, написанные под старый и новый путь, работают вместе. Имена не менялись.
package bot

import (
	"github.com/arrowwhi/infrastructure/telegram"
	"github.com/arrowwhi/infrastructure/telegram/api"
	"github.com/arrowwhi/infrastructure/telegram/state"
)

// MaxCallbackData — см. [telegram.MaxCallbackData].
const MaxCallbackData = telegram.MaxCallbackData

// Режимы получения обновлений.
const (
	ModePolling = telegram.ModePolling
	ModeWebhook = telegram.ModeWebhook
)

type (
	Bot           = telegram.Bot
	Config        = telegram.Config
	WebhookConfig = telegram.WebhookConfig
	Context       = telegram.Context
	HandlerFunc   = telegram.HandlerFunc
	Middleware    = telegram.Middleware
	ErrorHandler  = telegram.ErrorHandler
	Option        = telegram.Option
	SendOption    = telegram.SendOption
)

// New вызывает [telegram.New].
func New(cfg Config, opts ...Option) (*Bot, error) { return telegram.New(cfg, opts...) }

// DefaultConfig вызывает [telegram.DefaultConfig].
func DefaultConfig(token string) Config { return telegram.DefaultConfig(token) }

// CallbackData вызывает [telegram.CallbackData].
func CallbackData(prefix string, args ...string) (string, error) {
	return telegram.CallbackData(prefix, args...)
}

// MustCallbackData вызывает [telegram.MustCallbackData].
func MustCallbackData(prefix string, args ...string) string {
	return telegram.MustCallbackData(prefix, args...)
}

// WithStateStore вызывает [telegram.WithStateStore].
func WithStateStore(store state.Store) Option { return telegram.WithStateStore(store) }

// WithAPIClient вызывает [telegram.WithAPIClient].
func WithAPIClient(c *api.Client) Option { return telegram.WithAPIClient(c) }

// WithErrorHandler вызывает [telegram.WithErrorHandler].
func WithErrorHandler(h ErrorHandler) Option { return telegram.WithErrorHandler(h) }

// ReplyOnError вызывает [telegram.ReplyOnError].
func ReplyOnError(text string) ErrorHandler { return telegram.ReplyOnError(text) }

// WithKeyboard вызывает [telegram.WithKeyboard].
func WithKeyboard(markup api.ReplyMarkup) SendOption { return telegram.WithKeyboard(markup) }

// WithParseMode вызывает [telegram.WithParseMode].
func WithParseMode(mode string) SendOption { return telegram.WithParseMode(mode) }

// WithoutPreview вызывает [telegram.WithoutPreview].
func WithoutPreview() SendOption { return telegram.WithoutPreview() }

// Silent вызывает [telegram.Silent].
func Silent() SendOption { return telegram.Silent() }
