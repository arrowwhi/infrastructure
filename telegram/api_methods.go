package telegram

import (
	"context"
	"time"
)

const methodGetUpdates = "getUpdates"

// GetMe возвращает информацию о боте. Годится как health-проверка токена и связи.
func (c *Client) GetMe(ctx context.Context) (User, error) {
	var u User
	err := c.Call(ctx, "getMe", 0, struct{}{}, &u)

	return u, err
}

// Healthcheck — проверка для observability/health.
func (c *Client) Healthcheck(ctx context.Context) error {
	_, err := c.GetMe(ctx)

	return err
}

// GetUpdates получает обновления long polling. Контекст ограничивается
// req.Timeout + запас, локальные лимиты на этот метод не действуют.
func (c *Client) GetUpdates(ctx context.Context, req GetUpdatesRequest) ([]Update, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second+pollMargin)
	defer cancel()

	var updates []Update
	if err := c.Call(ctx, methodGetUpdates, 0, req, &updates); err != nil {
		return nil, err
	}

	return updates, nil
}

// SetWebhook включает доставку обновлений на HTTPS-адрес.
func (c *Client) SetWebhook(ctx context.Context, req SetWebhookRequest) error {
	return c.Call(ctx, "setWebhook", 0, req, nil)
}

// DeleteWebhook выключает webhook (нужно перед getUpdates).
func (c *Client) DeleteWebhook(ctx context.Context, dropPending bool) error {
	return c.Call(ctx, "deleteWebhook", 0, map[string]bool{"drop_pending_updates": dropPending}, nil)
}

// SendMessage отправляет текстовое сообщение.
func (c *Client) SendMessage(ctx context.Context, req SendMessageRequest) (Message, error) {
	var m Message
	err := c.Call(ctx, "sendMessage", req.ChatID, req, &m)

	return m, err
}

// SendPhoto отправляет фотографию.
func (c *Client) SendPhoto(ctx context.Context, req SendPhotoRequest) (Message, error) {
	var m Message
	err := c.Call(ctx, "sendPhoto", req.ChatID, req, &m)

	return m, err
}

// EditMessageText меняет текст сообщения бота.
func (c *Client) EditMessageText(ctx context.Context, req EditMessageTextRequest) error {
	return c.Call(ctx, "editMessageText", req.ChatID, req, nil)
}

// EditMessageReplyMarkup меняет или убирает (markup == nil) клавиатуру сообщения.
func (c *Client) EditMessageReplyMarkup(ctx context.Context, chatID int64, messageID int, markup *InlineKeyboardMarkup) error {
	payload := map[string]any{"chat_id": chatID, "message_id": messageID}
	if markup != nil {
		payload["reply_markup"] = markup
	}

	return c.Call(ctx, "editMessageReplyMarkup", chatID, payload, nil)
}

// DeleteMessage удаляет сообщение.
func (c *Client) DeleteMessage(ctx context.Context, chatID int64, messageID int) error {
	return c.Call(ctx, "deleteMessage", chatID, map[string]any{"chat_id": chatID, "message_id": messageID}, nil)
}

// AnswerCallbackQuery отвечает на нажатие кнопки (убирает «часики» на кнопке).
func (c *Client) AnswerCallbackQuery(ctx context.Context, req AnswerCallbackQueryRequest) error {
	return c.Call(ctx, "answerCallbackQuery", 0, req, nil)
}

// SendChatAction показывает статус «печатает…» и т.п. (action: typing, upload_photo…).
func (c *Client) SendChatAction(ctx context.Context, chatID int64, action string) error {
	return c.Call(ctx, "sendChatAction", 0, map[string]any{"chat_id": chatID, "action": action}, nil)
}

// SetMyCommands задаёт меню команд бота.
func (c *Client) SetMyCommands(ctx context.Context, commands []BotCommand) error {
	return c.Call(ctx, "setMyCommands", 0, map[string]any{"commands": commands}, nil)
}
