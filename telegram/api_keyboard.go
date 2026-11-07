package telegram

// InlineKeyboard собирает inline-клавиатуру из рядов кнопок.
//
//	telegram.InlineKeyboard(
//	    telegram.Row(telegram.CallbackButton("Да", "ok:42"), telegram.CallbackButton("Нет", "no:42")),
//	    telegram.Row(telegram.URLButton("Открыть", "https://example.com")),
//	)
func InlineKeyboard(rows ...[]InlineKeyboardButton) *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

// Row — ряд кнопок.
func Row(buttons ...InlineKeyboardButton) []InlineKeyboardButton {
	return buttons
}

// CallbackButton — кнопка, присылающая боту data (не длиннее 64 байт).
func CallbackButton(text, data string) InlineKeyboardButton {
	return InlineKeyboardButton{Text: text, CallbackData: data}
}

// URLButton — кнопка-ссылка.
func URLButton(text, url string) InlineKeyboardButton {
	return InlineKeyboardButton{Text: text, URL: url}
}

// WebAppButton — кнопка, открывающая Mini App.
func WebAppButton(text, url string) InlineKeyboardButton {
	return InlineKeyboardButton{Text: text, WebApp: &WebAppInfo{URL: url}}
}
