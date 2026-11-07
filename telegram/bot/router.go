package bot

import (
	"fmt"
	"sort"
	"strings"

	"github.com/arrowwhi/infrastructure/telegram/api"
)

const (
	callbackSeparator = ":"
	// MaxCallbackData — лимит Telegram на callback_data в байтах.
	MaxCallbackData = 64
)

// HandlerFunc обрабатывает обновление. Возвращённая ошибка логируется, попадает
// в метрики и передаётся в ErrorHandler.
type HandlerFunc func(c *Context) error

// Middleware оборачивает обработчик.
type Middleware func(next HandlerFunc) HandlerFunc

type command struct {
	handler     HandlerFunc
	description string
}

type callbackRoute struct {
	handler HandlerFunc
	prefix  string
}

// router хранит маршруты. Регистрация — до Run, поэтому без блокировок.
type router struct {
	commands  map[string]command
	states    map[string]HandlerFunc
	text      HandlerFunc
	fallback  HandlerFunc
	callbacks []callbackRoute
}

func newRouter() *router {
	return &router{commands: map[string]command{}, states: map[string]HandlerFunc{}}
}

// Command регистрирует обработчик команды /name. Команды с непустым description
// попадают в меню бота. Команда срабатывает в любом состоянии диалога.
func (b *Bot) Command(name, description string, h HandlerFunc) {
	b.router.commands[strings.TrimPrefix(strings.ToLower(name), "/")] = command{handler: h, description: description}
}

// Callback регистрирует обработчик кнопок, чья data равна prefix или начинается с "prefix:".
// При нескольких подходящих префиксах выбирается самый длинный.
func (b *Bot) Callback(prefix string, h HandlerFunc) {
	b.router.callbacks = append(b.router.callbacks, callbackRoute{prefix: prefix, handler: h})
	sort.SliceStable(b.router.callbacks, func(i, j int) bool {
		return len(b.router.callbacks[i].prefix) > len(b.router.callbacks[j].prefix)
	})
}

// OnState регистрирует обработчик сообщений, пришедших, пока диалог в шаге name
// (выставляется через Context.SetState). Имеет приоритет над Text.
func (b *Bot) OnState(name string, h HandlerFunc) {
	b.router.states[name] = h
}

// Text регистрирует обработчик обычных сообщений (не команд и вне шагов диалога).
func (b *Bot) Text(h HandlerFunc) {
	b.router.text = h
}

// Default регистрирует обработчик всего, что не подошло ни под один маршрут.
// Без него такие обновления только учитываются в метриках как unhandled.
func (b *Bot) Default(h HandlerFunc) {
	b.router.fallback = h
}

// Use добавляет middleware, которые выполняются после встроенных (recovery,
// трейсинг, логирование, метрики) и перед обработчиком.
func (b *Bot) Use(mws ...Middleware) {
	b.middlewares = append(b.middlewares, mws...)
}

// CallbackData собирает data кнопки "prefix:arg1:arg2" и проверяет лимит в 64 байта.
func CallbackData(prefix string, args ...string) (string, error) {
	data := strings.Join(append([]string{prefix}, args...), callbackSeparator)
	if len(data) > MaxCallbackData {
		return "", fmt.Errorf("bot: callback data %q is %d bytes, Telegram allows %d", data, len(data), MaxCallbackData)
	}

	return data, nil
}

// MustCallbackData — CallbackData с паникой при превышении лимита (для констант).
func MustCallbackData(prefix string, args ...string) string {
	data, err := CallbackData(prefix, args...)
	if err != nil {
		panic(err)
	}

	return data
}

// resolve выбирает обработчик и заполняет поля Context, связанные с маршрутом.
func (b *Bot) resolve(c *Context) (HandlerFunc, error) {
	r := b.router

	if cb := c.Update.CallbackQuery; cb != nil {
		for _, route := range r.callbacks {
			if cb.Data == route.prefix || strings.HasPrefix(cb.Data, route.prefix+callbackSeparator) {
				c.route, c.cbPrefix = "callback:"+route.prefix, route.prefix

				return route.handler, nil
			}
		}

		return b.unhandled(c), nil
	}

	msg := c.Update.Message
	if msg == nil {
		return b.unhandled(c), nil
	}

	if name, args, ok := b.parseCommand(msg); ok {
		c.command, c.args = name, args

		if cmd, found := r.commands[name]; found {
			c.route = "command:" + name

			return cmd.handler, nil
		}

		return b.unhandled(c), nil
	}

	if len(r.states) > 0 {
		s, ok, err := c.State()
		if err != nil {
			return nil, fmt.Errorf("load state: %w", err)
		}

		if h, found := r.states[s.Name]; ok && found {
			c.route = "state:" + s.Name

			return h, nil
		}
	}

	if r.text != nil && c.Text() != "" {
		c.route = "text"

		return r.text, nil
	}

	return b.unhandled(c), nil
}

func (b *Bot) unhandled(c *Context) HandlerFunc {
	if b.router.fallback != nil {
		c.route = "default"

		return b.router.fallback
	}

	c.route = routeUnhandled

	return nil
}

const routeUnhandled = "unhandled"

// parseCommand разбирает "/cmd@bot args". Команды, адресованные другому боту, пропускаются.
func (b *Bot) parseCommand(m *api.Message) (name, args string, ok bool) {
	text := m.Text
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}

	head, rest, _ := strings.Cut(text, " ")
	head = strings.TrimPrefix(head, "/")

	if cmd, mention, found := strings.Cut(head, "@"); found {
		if b.username != "" && !strings.EqualFold(mention, b.username) {
			return "", "", false
		}

		head = cmd
	}

	if head == "" {
		return "", "", false
	}

	return strings.ToLower(head), strings.TrimSpace(rest), true
}

// menuCommands возвращает команды с описанием для setMyCommands.
func (r *router) menuCommands() []api.BotCommand {
	var out []api.BotCommand

	for name, cmd := range r.commands {
		if cmd.description != "" {
			out = append(out, api.BotCommand{Command: name, Description: cmd.description})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Command < out[j].Command })

	return out
}
