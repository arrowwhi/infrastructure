// Package api — прежний путь клиента Bot API.
//
// Deprecated: пакет оставлен для обратной совместимости, весь код переехал в
// [github.com/arrowwhi/infrastructure/telegram]. Все типы здесь — алиасы, поэтому
// значения свободно передаются между старым и новым путём. Новые имена:
// Config → ClientConfig, DefaultConfig → DefaultClientConfig, Option → ClientOption,
// New → NewClient, Error → APIError.
package api

import (
	"net/http"

	"github.com/arrowwhi/infrastructure/telegram"
)

// Parse modes.
const (
	ParseModeHTML       = telegram.ParseModeHTML
	ParseModeMarkdownV2 = telegram.ParseModeMarkdownV2
)

type (
	// Config — алиас [telegram.ClientConfig].
	Config = telegram.ClientConfig
	// Error — алиас [telegram.APIError].
	Error = telegram.APIError
	// Client — алиас [telegram.Client].
	Client = telegram.Client
	// Option — алиас [telegram.ClientOption].
	Option = telegram.ClientOption

	Update                     = telegram.Update
	Message                    = telegram.Message
	MessageEntity              = telegram.MessageEntity
	PhotoSize                  = telegram.PhotoSize
	CallbackQuery              = telegram.CallbackQuery
	User                       = telegram.User
	Chat                       = telegram.Chat
	ReplyMarkup                = telegram.ReplyMarkup
	InlineKeyboardMarkup       = telegram.InlineKeyboardMarkup
	InlineKeyboardButton       = telegram.InlineKeyboardButton
	WebAppInfo                 = telegram.WebAppInfo
	ReplyKeyboardMarkup        = telegram.ReplyKeyboardMarkup
	KeyboardButton             = telegram.KeyboardButton
	ReplyKeyboardRemove        = telegram.ReplyKeyboardRemove
	LinkPreviewOptions         = telegram.LinkPreviewOptions
	SendMessageRequest         = telegram.SendMessageRequest
	SendPhotoRequest           = telegram.SendPhotoRequest
	EditMessageTextRequest     = telegram.EditMessageTextRequest
	AnswerCallbackQueryRequest = telegram.AnswerCallbackQueryRequest
	GetUpdatesRequest          = telegram.GetUpdatesRequest
	SetWebhookRequest          = telegram.SetWebhookRequest
	BotCommand                 = telegram.BotCommand
)

// DefaultConfig вызывает [telegram.DefaultClientConfig].
func DefaultConfig(token string) Config { return telegram.DefaultClientConfig(token) }

// New вызывает [telegram.NewClient].
func New(cfg Config, opts ...Option) (*Client, error) { return telegram.NewClient(cfg, opts...) }

// WithHTTPClient вызывает [telegram.WithHTTPClient].
func WithHTTPClient(c *http.Client) Option { return telegram.WithHTTPClient(c) }

// IsCode вызывает [telegram.IsCode].
func IsCode(err error, code int) bool { return telegram.IsCode(err, code) }

// IsMessageNotModified вызывает [telegram.IsMessageNotModified].
func IsMessageNotModified(err error) bool { return telegram.IsMessageNotModified(err) }

// InlineKeyboard вызывает [telegram.InlineKeyboard].
func InlineKeyboard(rows ...[]InlineKeyboardButton) *InlineKeyboardMarkup {
	return telegram.InlineKeyboard(rows...)
}

// Row вызывает [telegram.Row].
func Row(buttons ...InlineKeyboardButton) []InlineKeyboardButton { return telegram.Row(buttons...) }

// CallbackButton вызывает [telegram.CallbackButton].
func CallbackButton(text, data string) InlineKeyboardButton {
	return telegram.CallbackButton(text, data)
}

// URLButton вызывает [telegram.URLButton].
func URLButton(text, url string) InlineKeyboardButton { return telegram.URLButton(text, url) }

// WebAppButton вызывает [telegram.WebAppButton].
func WebAppButton(text, url string) InlineKeyboardButton { return telegram.WebAppButton(text, url) }
