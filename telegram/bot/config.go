package bot

import (
	"net"
	"time"

	"github.com/arrowwhi/infrastructure/telegram/api"
)

// Режимы получения обновлений.
const (
	// ModePolling — long polling (getUpdates). Не требует публичного адреса: локальная разработка.
	ModePolling = "polling"
	// ModeWebhook — Telegram сам присылает обновления на HTTPS-адрес: продакшен.
	ModeWebhook = "webhook"
)

// Config — настройки бота.
type Config struct {
	Webhook WebhookConfig `yaml:"webhook"`
	Mode    string        `yaml:"mode" env:"TELEGRAM_MODE" env-default:"polling"`
	API     api.Config    `yaml:"api"`

	// PollTimeout — сколько getUpdates ждёт новых обновлений (long polling).
	PollTimeout time.Duration `yaml:"poll_timeout" env:"TELEGRAM_POLL_TIMEOUT" env-default:"30s"`

	// Workers — число параллельных обработчиков. Обновления одного чата всегда
	// обрабатываются по порядку одним воркером, разные чаты — параллельно.
	Workers int `yaml:"workers" env:"TELEGRAM_WORKERS" env-default:"8"`
	// QueueSize — размер очереди каждого воркера. При переполнении приём обновлений
	// притормаживает (polling ждёт, webhook отвечает позже).
	QueueSize int `yaml:"queue_size" env:"TELEGRAM_QUEUE_SIZE" env-default:"128"`

	// HandlerTimeout — дедлайн обработки одного обновления.
	HandlerTimeout time.Duration `yaml:"handler_timeout" env:"TELEGRAM_HANDLER_TIMEOUT" env-default:"30s"`

	// StateTTL — срок жизни состояния диалога, выставленного через Context.SetState.
	StateTTL time.Duration `yaml:"state_ttl" env:"TELEGRAM_STATE_TTL" env-default:"24h"`

	// SetCommands — публиковать меню команд (setMyCommands) из зарегистрированных команд с описанием.
	SetCommands bool `yaml:"set_commands" env:"TELEGRAM_SET_COMMANDS" env-default:"true"`
}

// WebhookConfig — настройки режима webhook.
type WebhookConfig struct {
	// URL — публичный HTTPS-адрес, который увидит Telegram (с путём), например
	// https://bot.example.com/telegram/webhook.
	URL string `yaml:"url" env:"TELEGRAM_WEBHOOK_URL"`
	// Secret — секрет из заголовка X-Telegram-Bot-Api-Secret-Token. Обязателен:
	// без него кто угодно, зная адрес, пришлёт боту поддельное обновление.
	Secret string `yaml:"secret" env:"TELEGRAM_WEBHOOK_SECRET"`

	// Host, Port, Path — где слушает встроенный HTTP-сервер (за reverse proxy).
	// Пустой Port — встроенный сервер не поднимается, Bot.WebhookHandler монтируется
	// на HTTP-сервер сервиса.
	Host string `yaml:"host" env:"TELEGRAM_WEBHOOK_HOST" env-default:"0.0.0.0"`
	Port string `yaml:"port" env:"TELEGRAM_WEBHOOK_PORT" env-default:"8443"`
	Path string `yaml:"path" env:"TELEGRAM_WEBHOOK_PATH" env-default:"/telegram/webhook"`

	// DropPendingUpdates — отбросить обновления, накопившиеся, пока бот лежал.
	DropPendingUpdates bool `yaml:"drop_pending_updates" env:"TELEGRAM_WEBHOOK_DROP_PENDING" env-default:"false"`
}

// Addr возвращает адрес встроенного webhook-сервера.
func (c WebhookConfig) Addr() string {
	return net.JoinHostPort(c.Host, c.Port)
}

// DefaultConfig возвращает настройки по умолчанию (совпадают с env-default).
func DefaultConfig(token string) Config {
	return Config{
		API:            api.DefaultConfig(token),
		Mode:           ModePolling,
		PollTimeout:    30 * time.Second, //nolint:mnd
		Workers:        8,                //nolint:mnd
		QueueSize:      128,              //nolint:mnd
		HandlerTimeout: 30 * time.Second, //nolint:mnd
		StateTTL:       24 * time.Hour,   //nolint:mnd
		SetCommands:    true,
		Webhook: WebhookConfig{
			Host: "0.0.0.0",
			Port: "8443",
			Path: "/telegram/webhook",
		},
	}
}

// allowedUpdates — какие типы обновлений запрашивать у Telegram.
var allowedUpdates = []string{"message", "callback_query"}
