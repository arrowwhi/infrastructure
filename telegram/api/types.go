package api

import "encoding/json"

// Update — входящее обновление. Поддерживаются сообщения, их правки и нажатия
// inline-кнопок; остальные типы можно получить через Raw.
type Update struct {
	Message       *Message       `json:"message,omitempty"`
	EditedMessage *Message       `json:"edited_message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
	UpdateID      int64          `json:"update_id"`
}

// Type возвращает тип обновления: message, edited_message, callback_query или unknown.
func (u Update) Type() string {
	switch {
	case u.Message != nil:
		return "message"
	case u.EditedMessage != nil:
		return "edited_message"
	case u.CallbackQuery != nil:
		return "callback_query"
	default:
		return "unknown"
	}
}

// Chat возвращает чат обновления (nil, если его нет).
func (u Update) Chat() *Chat {
	switch {
	case u.Message != nil:
		return &u.Message.Chat
	case u.EditedMessage != nil:
		return &u.EditedMessage.Chat
	case u.CallbackQuery != nil && u.CallbackQuery.Message != nil:
		return &u.CallbackQuery.Message.Chat
	default:
		return nil
	}
}

// From возвращает автора обновления (nil, если его нет).
func (u Update) From() *User {
	switch {
	case u.Message != nil:
		return u.Message.From
	case u.EditedMessage != nil:
		return u.EditedMessage.From
	case u.CallbackQuery != nil:
		return &u.CallbackQuery.From
	default:
		return nil
	}
}

// Message — сообщение.
type Message struct {
	From           *User           `json:"from,omitempty"`
	ReplyToMessage *Message        `json:"reply_to_message,omitempty"`
	Text           string          `json:"text,omitempty"`
	Caption        string          `json:"caption,omitempty"`
	Entities       []MessageEntity `json:"entities,omitempty"`
	Photo          []PhotoSize     `json:"photo,omitempty"`
	Chat           Chat            `json:"chat"`
	Date           int64           `json:"date"`
	MessageID      int             `json:"message_id"`
}

// MessageEntity — разметка внутри текста (команды, ссылки, упоминания).
type MessageEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

// PhotoSize — один из размеров фотографии.
type PhotoSize struct {
	FileID   string `json:"file_id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	FileSize int    `json:"file_size,omitempty"`
}

// CallbackQuery — нажатие inline-кнопки.
type CallbackQuery struct {
	Message *Message `json:"message,omitempty"`
	ID      string   `json:"id"`
	Data    string   `json:"data,omitempty"`
	From    User     `json:"from"`
}

// User — пользователь или бот.
type User struct {
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name,omitempty"`
	Username     string `json:"username,omitempty"`
	LanguageCode string `json:"language_code,omitempty"`
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot"`
}

// Chat — чат. ID отрицательный у групп и каналов.
type Chat struct {
	Type     string `json:"type"`
	Title    string `json:"title,omitempty"`
	Username string `json:"username,omitempty"`
	ID       int64  `json:"id"`
}

// IsPrivate сообщает, личный ли это чат.
func (c Chat) IsPrivate() bool { return c.Type == "private" }

// Режимы разметки текста.
const (
	ParseModeHTML       = "HTML"
	ParseModeMarkdownV2 = "MarkdownV2"
)

// ReplyMarkup — клавиатура сообщения: *InlineKeyboardMarkup, *ReplyKeyboardMarkup
// или *ReplyKeyboardRemove.
type ReplyMarkup interface {
	replyMarkup()
}

// InlineKeyboardMarkup — клавиатура под сообщением.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

func (*InlineKeyboardMarkup) replyMarkup() {}

// InlineKeyboardButton — кнопка inline-клавиатуры.
type InlineKeyboardButton struct {
	WebApp       *WebAppInfo `json:"web_app,omitempty"`
	Text         string      `json:"text"`
	CallbackData string      `json:"callback_data,omitempty"`
	URL          string      `json:"url,omitempty"`
}

// WebAppInfo — ссылка на Mini App.
type WebAppInfo struct {
	URL string `json:"url"`
}

// ReplyKeyboardMarkup — клавиатура вместо поля ввода.
type ReplyKeyboardMarkup struct {
	Keyboard        [][]KeyboardButton `json:"keyboard"`
	ResizeKeyboard  bool               `json:"resize_keyboard,omitempty"`
	OneTimeKeyboard bool               `json:"one_time_keyboard,omitempty"`
}

func (*ReplyKeyboardMarkup) replyMarkup() {}

// KeyboardButton — кнопка обычной клавиатуры.
type KeyboardButton struct {
	Text string `json:"text"`
}

// ReplyKeyboardRemove убирает обычную клавиатуру.
type ReplyKeyboardRemove struct {
	RemoveKeyboard bool `json:"remove_keyboard"`
}

func (*ReplyKeyboardRemove) replyMarkup() {}

// LinkPreviewOptions — настройки превью ссылок.
type LinkPreviewOptions struct {
	IsDisabled bool `json:"is_disabled"`
}

// SendMessageRequest — параметры sendMessage.
type SendMessageRequest struct {
	ReplyMarkup        ReplyMarkup         `json:"reply_markup,omitempty"`
	LinkPreviewOptions *LinkPreviewOptions `json:"link_preview_options,omitempty"`
	Text               string              `json:"text"`
	ParseMode          string              `json:"parse_mode,omitempty"`
	ChatID             int64               `json:"chat_id"`
	ReplyToMessageID   int                 `json:"reply_to_message_id,omitempty"`
	DisableNotify      bool                `json:"disable_notification,omitempty"`
}

// SendPhotoRequest — параметры sendPhoto. Photo — file_id или HTTP URL.
type SendPhotoRequest struct {
	ReplyMarkup ReplyMarkup `json:"reply_markup,omitempty"`
	Photo       string      `json:"photo"`
	Caption     string      `json:"caption,omitempty"`
	ParseMode   string      `json:"parse_mode,omitempty"`
	ChatID      int64       `json:"chat_id"`
}

// EditMessageTextRequest — параметры editMessageText.
type EditMessageTextRequest struct {
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	Text        string                `json:"text"`
	ParseMode   string                `json:"parse_mode,omitempty"`
	ChatID      int64                 `json:"chat_id"`
	MessageID   int                   `json:"message_id"`
}

// AnswerCallbackQueryRequest — параметры answerCallbackQuery.
type AnswerCallbackQueryRequest struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
	ShowAlert       bool   `json:"show_alert,omitempty"`
}

// GetUpdatesRequest — параметры getUpdates.
type GetUpdatesRequest struct {
	AllowedUpdates []string `json:"allowed_updates,omitempty"`
	Offset         int64    `json:"offset,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	Timeout        int      `json:"timeout,omitempty"` // секунды long polling
}

// SetWebhookRequest — параметры setWebhook.
type SetWebhookRequest struct {
	URL                string   `json:"url"`
	SecretToken        string   `json:"secret_token,omitempty"`
	AllowedUpdates     []string `json:"allowed_updates,omitempty"`
	MaxConnections     int      `json:"max_connections,omitempty"`
	DropPendingUpdates bool     `json:"drop_pending_updates,omitempty"`
}

// BotCommand — команда в меню бота.
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// response — конверт ответа Bot API.
type response struct {
	Parameters  *responseParameters `json:"parameters,omitempty"`
	Description string              `json:"description"`
	Result      json.RawMessage     `json:"result"`
	ErrorCode   int                 `json:"error_code"`
	OK          bool                `json:"ok"`
}

type responseParameters struct {
	RetryAfter      int   `json:"retry_after,omitempty"`
	MigrateToChatID int64 `json:"migrate_to_chat_id,omitempty"`
}
