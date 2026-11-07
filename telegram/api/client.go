// Package api is a hand-written Telegram Bot API client.
//
// Клиент намеренно небольшой и без собственного диспетчера: роутинг обновлений
// живёт в telegram/bot. Зато клиент сам соблюдает лимиты Telegram (глобальный и
// на чат), повторяет запрос после 429 с паузой из retry_after, не пишет токен
// бота в ошибки и снимает метрики и трейсы каждого вызова.
//
// Методы, для которых нет обёртки, вызываются через Client.Call.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/arrowwhi/infrastructure/observability/logger"
)

const (
	instrumentationName = "github.com/arrowwhi/infrastructure/telegram/api"
	maxResponseBytes    = 8 << 20
	// pollMargin — запас поверх таймаута long polling: сам запрос висит до
	// timeout, обрывать его раньше нельзя.
	pollMargin = 10 * time.Second
)

// Config — настройки клиента Bot API.
type Config struct {
	Token   string `yaml:"token" env:"TELEGRAM_BOT_TOKEN" env-required:"true"`
	BaseURL string `yaml:"base_url" env:"TELEGRAM_API_URL" env-default:"https://api.telegram.org"`

	// RequestTimeout — таймаут обычного вызова (getUpdates считает свой).
	RequestTimeout time.Duration `yaml:"request_timeout" env:"TELEGRAM_REQUEST_TIMEOUT" env-default:"15s"`

	// GlobalRate — не больше стольких исходящих сообщений в секунду на бота (Telegram: ~30).
	GlobalRate float64 `yaml:"global_rate" env:"TELEGRAM_GLOBAL_RATE" env-default:"30"`
	// ChatInterval — минимальный интервал между сообщениями в один личный чат (Telegram: ~1 с).
	ChatInterval time.Duration `yaml:"chat_interval" env:"TELEGRAM_CHAT_INTERVAL" env-default:"1s"`
	// GroupInterval — то же для групп (Telegram: 20 сообщений в минуту).
	GroupInterval time.Duration `yaml:"group_interval" env:"TELEGRAM_GROUP_INTERVAL" env-default:"3s"`
	// ChatBurst — сколько сообщений в чат можно отправить подряд без ожидания.
	ChatBurst int `yaml:"chat_burst" env:"TELEGRAM_CHAT_BURST" env-default:"3"`

	// MaxRetries — сколько раз повторять запрос после 429 Too Many Requests.
	MaxRetries int `yaml:"max_retries" env:"TELEGRAM_MAX_RETRIES" env-default:"3"`
}

// DefaultConfig возвращает настройки по умолчанию (совпадают с env-default).
func DefaultConfig(token string) Config {
	return Config{
		Token:          token,
		BaseURL:        "https://api.telegram.org",
		RequestTimeout: 15 * time.Second, //nolint:mnd
		GlobalRate:     30,               //nolint:mnd
		ChatInterval:   time.Second,
		GroupInterval:  3 * time.Second, //nolint:mnd
		ChatBurst:      3,               //nolint:mnd
		MaxRetries:     3,               //nolint:mnd
	}
}

// Error — отказ Bot API. Различайте ошибки по Code через errors.As, а не по тексту.
type Error struct {
	Method      string
	Description string
	Code        int
	RetryAfter  time.Duration
}

func (e *Error) Error() string {
	return fmt.Sprintf("telegram %s failed (%d): %s", e.Method, e.Code, e.Description)
}

// IsCode сообщает, что err — ошибка Bot API с указанным кодом.
func IsCode(err error, code int) bool {
	var apiErr *Error

	return errors.As(err, &apiErr) && apiErr.Code == code
}

// IsMessageNotModified сообщает, что правка не изменила сообщение (ошибкой это обычно не считают).
func IsMessageNotModified(err error) bool {
	var apiErr *Error

	return errors.As(err, &apiErr) && strings.Contains(apiErr.Description, "message is not modified")
}

// Client — клиент Bot API. Безопасен для конкурентного использования.
type Client struct {
	http    *http.Client
	limiter *limiter
	tracer  trace.Tracer
	metrics *clientMetrics
	cfg     Config
}

// Option настраивает клиент.
type Option func(*Client)

// WithHTTPClient подменяет HTTP-клиент (прокси, тесты).
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) { cl.http = c }
}

// New создаёт клиента.
func New(cfg Config, opts ...Option) (*Client, error) {
	if cfg.Token == "" {
		return nil, errors.New("telegram: bot token is required")
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.telegram.org"
	}

	m, err := newClientMetrics(otel.Meter(instrumentationName))
	if err != nil {
		return nil, err
	}

	c := &Client{
		cfg:     cfg,
		http:    &http.Client{},
		limiter: newLimiter(cfg),
		tracer:  otel.Tracer(instrumentationName),
		metrics: m,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// Call вызывает произвольный метод Bot API. payload сериализуется в JSON, result
// декодируется в out (может быть nil). chatID > 0 включает лимит на чат — передавайте
// его для методов, отправляющих или изменяющих сообщения.
func (c *Client) Call(ctx context.Context, method string, chatID int64, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", method, err)
	}

	ctx, span := c.tracer.Start(ctx, "telegram."+method,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("telegram.method", method)),
	)
	defer span.End()

	if chatID != 0 {
		span.SetAttributes(attribute.Int64("telegram.chat_id", chatID))
	}

	for attempt := 0; ; attempt++ {
		if method != methodGetUpdates {
			waited, waitErr := c.limiter.wait(ctx, chatID)
			if waitErr != nil {
				return c.finish(span, waitErr)
			}

			c.metrics.recordThrottle(ctx, method, waited)
		}

		err = c.do(ctx, method, body, out)

		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Code != http.StatusTooManyRequests || attempt >= c.cfg.MaxRetries {
			return c.finish(span, err)
		}

		c.metrics.rateLimited.Add(ctx, 1, metric.WithAttributes(attribute.String("method", method)))

		logger.Warn(ctx).
			Str("method", method).
			Int64("chat_id", chatID).
			Dur("retry_after", apiErr.RetryAfter).
			Msg("Telegram rate limit hit, retrying")

		select {
		case <-ctx.Done():
			return c.finish(span, ctx.Err())
		case <-time.After(apiErr.RetryAfter):
		}
	}
}

func (c *Client) finish(span trace.Span, err error) error {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	return err
}

// do выполняет одну попытку вызова и пишет метрики.
func (c *Client) do(ctx context.Context, method string, body []byte, out any) (err error) {
	start := time.Now()

	defer func() {
		c.metrics.recordCall(ctx, method, start, err)
	}()

	if method != methodGetUpdates && c.cfg.RequestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.RequestTimeout)

		defer cancel()
	}

	endpoint := strings.TrimRight(c.cfg.BaseURL, "/") + "/bot" + c.cfg.Token + "/" + method

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build %s request: %w", method, redact(err))
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s request failed: %w", method, redact(err))
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read %s response: %w", method, err)
	}

	var parsed response
	if err = json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("decode %s response (HTTP %d): %w", method, resp.StatusCode, err)
	}

	if !parsed.OK {
		apiErr := &Error{Method: method, Code: parsed.ErrorCode, Description: parsed.Description}
		if parsed.Parameters != nil && parsed.Parameters.RetryAfter > 0 {
			apiErr.RetryAfter = time.Duration(parsed.Parameters.RetryAfter) * time.Second
		}

		return apiErr
	}

	if out != nil && len(parsed.Result) > 0 {
		if err = json.Unmarshal(parsed.Result, out); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
	}

	return nil
}

// redact убирает URL из ошибки транспорта: в нём токен бота.
func redact(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}

	return err
}
