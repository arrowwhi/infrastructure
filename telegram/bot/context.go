package bot

import (
	"context"
	"errors"
	"strings"

	"github.com/arrowwhi/infrastructure/telegram/api"
	"github.com/arrowwhi/infrastructure/telegram/state"
)

// Context — обновление и всё, что нужно обработчику, чтобы на него ответить.
// Встраивает context.Context: его можно передавать в репозитории и клиенты как есть.
type Context struct {
	context.Context //nolint:containedctx // Context — это и есть контекст обработки обновления

	bot      *Bot
	Update   api.Update
	route    string
	command  string
	args     string
	cbPrefix string
	st       state.State
	stLoaded bool
	hasState bool
	answered bool
}

// Bot возвращает бота.
func (c *Context) Bot() *Bot { return c.bot }

// API возвращает клиент Bot API — для вызовов, которых нет среди хелперов.
func (c *Context) API() *api.Client { return c.bot.api }

// Route возвращает имя сработавшего маршрута (например, "command:start").
func (c *Context) Route() string { return c.route }

// Message возвращает сообщение (для нажатия кнопки — сообщение с кнопкой).
func (c *Context) Message() *api.Message {
	switch {
	case c.Update.Message != nil:
		return c.Update.Message
	case c.Update.CallbackQuery != nil:
		return c.Update.CallbackQuery.Message
	default:
		return nil
	}
}

// Callback возвращает нажатие кнопки или nil.
func (c *Context) Callback() *api.CallbackQuery { return c.Update.CallbackQuery }

// ChatID возвращает id чата (0, если чата нет).
func (c *Context) ChatID() int64 {
	if chat := c.Update.Chat(); chat != nil {
		return chat.ID
	}

	return 0
}

// UserID возвращает id автора обновления (0, если его нет).
func (c *Context) UserID() int64 {
	if u := c.Update.From(); u != nil {
		return u.ID
	}

	return 0
}

// User возвращает автора обновления.
func (c *Context) User() *api.User { return c.Update.From() }

// Text возвращает текст сообщения (или подпись к фото).
func (c *Context) Text() string {
	m := c.Update.Message
	if m == nil {
		return ""
	}

	if m.Text != "" {
		return m.Text
	}

	return m.Caption
}

// Command возвращает имя команды без "/" и "@bot" ("" — не команда).
func (c *Context) Command() string { return c.command }

// Args возвращает текст после команды: для "/rate 8 отлично" — "8 отлично".
func (c *Context) Args() string { return c.args }

// CallbackData возвращает data нажатой кнопки.
func (c *Context) CallbackData() string {
	if c.Update.CallbackQuery == nil {
		return ""
	}

	return c.Update.CallbackQuery.Data
}

// CallbackArgs возвращает части data после префикса маршрута: для кнопки
// CallbackData("rate", "42", "8") в обработчике Callback("rate", ...) — ["42", "8"].
func (c *Context) CallbackArgs() []string {
	data := strings.TrimPrefix(c.CallbackData(), c.cbPrefix)
	data = strings.TrimPrefix(data, callbackSeparator)

	if data == "" {
		return nil
	}

	return strings.Split(data, callbackSeparator)
}

// Reply отправляет текст в текущий чат.
func (c *Context) Reply(text string, opts ...SendOption) (api.Message, error) {
	req := api.SendMessageRequest{ChatID: c.ChatID(), Text: text}
	for _, opt := range opts {
		opt(&req)
	}

	return c.bot.api.SendMessage(c, req)
}

// Edit меняет текст сообщения с нажатой кнопкой (для callback) и его клавиатуру.
// Ошибку «message is not modified» не возвращает.
func (c *Context) Edit(text string, markup *api.InlineKeyboardMarkup, parseMode string) error {
	m := c.Message()
	if m == nil {
		return errors.New("bot: no message to edit")
	}

	err := c.bot.api.EditMessageText(c, api.EditMessageTextRequest{
		ChatID:      m.Chat.ID,
		MessageID:   m.MessageID,
		Text:        text,
		ParseMode:   parseMode,
		ReplyMarkup: markup,
	})
	if api.IsMessageNotModified(err) {
		return nil
	}

	return err
}

// Answer отвечает на нажатие кнопки (короткое всплывающее уведомление; text может быть пустым).
// Если обработчик не ответил сам, бот ответит пустым ответом после него — иначе
// у пользователя крутятся «часики» на кнопке.
func (c *Context) Answer(text string, alert bool) error {
	cb := c.Update.CallbackQuery
	if cb == nil || c.answered {
		return nil
	}

	c.answered = true

	return c.bot.api.AnswerCallbackQuery(c, api.AnswerCallbackQueryRequest{
		CallbackQueryID: cb.ID,
		Text:            text,
		ShowAlert:       alert,
	})
}

// StateKey возвращает ключ состояния текущего пользователя в текущем чате.
func (c *Context) StateKey() state.Key {
	return state.Key{ChatID: c.ChatID(), UserID: c.UserID()}
}

// State возвращает текущее состояние диалога. ok == false — состояния нет.
func (c *Context) State() (s state.State, ok bool, err error) {
	if c.stLoaded {
		return c.st, c.hasState, nil
	}

	s, err = c.bot.store.Get(c, c.StateKey())

	switch {
	case errors.Is(err, state.ErrNotFound):
		c.stLoaded, c.hasState = true, false

		return state.State{}, false, nil
	case err != nil:
		return state.State{}, false, err
	}

	c.st, c.stLoaded, c.hasState = s, true, true

	return s, true, nil
}

// SetState переводит диалог в шаг name с данными data (nil — без данных) на Config.StateTTL.
func (c *Context) SetState(name string, data any) error {
	s, err := state.New(name, data)
	if err != nil {
		return err
	}

	if err = c.bot.store.Set(c, c.StateKey(), s, c.bot.cfg.StateTTL); err != nil {
		return err
	}

	c.st, c.stLoaded, c.hasState = s, true, true

	return nil
}

// ClearState завершает диалог.
func (c *Context) ClearState() error {
	if err := c.bot.store.Delete(c, c.StateKey()); err != nil {
		return err
	}

	c.st, c.stLoaded, c.hasState = state.State{}, true, false

	return nil
}

// SendOption настраивает отправляемое сообщение.
type SendOption func(*api.SendMessageRequest)

// WithKeyboard прикрепляет клавиатуру.
func WithKeyboard(markup api.ReplyMarkup) SendOption {
	return func(r *api.SendMessageRequest) { r.ReplyMarkup = markup }
}

// WithParseMode задаёт разметку (api.ParseModeHTML, api.ParseModeMarkdownV2).
func WithParseMode(mode string) SendOption {
	return func(r *api.SendMessageRequest) { r.ParseMode = mode }
}

// WithoutPreview отключает превью ссылок.
func WithoutPreview() SendOption {
	return func(r *api.SendMessageRequest) {
		r.LinkPreviewOptions = &api.LinkPreviewOptions{IsDisabled: true}
	}
}

// Silent отправляет без звука.
func Silent() SendOption {
	return func(r *api.SendMessageRequest) { r.DisableNotify = true }
}
