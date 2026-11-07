package bot

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/arrowwhi/infrastructure/telegram/api"
)

const (
	secretHeader      = "X-Telegram-Bot-Api-Secret-Token" //nolint:gosec // имя заголовка, не секрет
	maxWebhookBody    = 1 << 20
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// WebhookHandler принимает обновления по HTTP. Его можно повесить на роутер сервиса
// вместо встроенного сервера: выставьте TELEGRAM_WEBHOOK_PORT пустым, и Run только
// зарегистрирует webhook и будет обрабатывать обновления. Запросы без верного
// секрета отклоняются с 401.
func (b *Bot) WebhookHandler() http.Handler {
	secret := []byte(b.cfg.Webhook.Secret)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)

			return
		}

		if subtle.ConstantTimeCompare([]byte(r.Header.Get(secretHeader)), secret) != 1 {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		var u api.Update
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxWebhookBody)).Decode(&u); err != nil {
			logger.Warn(r.Context()).Err(err).Msg("Failed to decode Telegram update")
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		// Ставим в очередь и сразу отвечаем 200: Telegram повторяет обновление, если
		// ответ задерживается. При переполненной очереди ответ притормозит — это и
		// есть обратное давление на Telegram.
		if err := b.dispatcher.enqueue(r.Context(), u); err != nil {
			// 503 — Telegram повторит доставку позже (например, во время рестарта).
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		w.WriteHeader(http.StatusOK)
	})
}

// runWebhook поднимает HTTP-сервер для обновлений и регистрирует webhook в Telegram.
func (b *Bot) runWebhook(ctx context.Context) error {
	cfg := b.cfg.Webhook

	if cfg.URL == "" || cfg.Secret == "" {
		return errors.New("bot: webhook mode requires TELEGRAM_WEBHOOK_URL and TELEGRAM_WEBHOOK_SECRET")
	}

	if cfg.Port == "" {
		// Встроенный сервер выключен: WebhookHandler смонтирован на роутер сервиса.
		return b.registerWebhookAndWait(ctx)
	}

	mux := http.NewServeMux()
	mux.Handle(cfg.Path, otelhttp.NewHandler(b.WebhookHandler(), "telegram.webhook"))

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	errCh := make(chan error, 1)

	go func() {
		logger.Info(ctx).Str("addr", srv.Addr).Str("path", cfg.Path).Msg("Starting Telegram webhook server")

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("webhook server: %w", err)
		}

		close(errCh)
	}()

	if err := b.api.SetWebhook(ctx, api.SetWebhookRequest{
		URL:                cfg.URL,
		SecretToken:        cfg.Secret,
		AllowedUpdates:     allowedUpdates,
		DropPendingUpdates: cfg.DropPendingUpdates,
	}); err != nil {
		_ = srv.Close()

		return fmt.Errorf("bot: setWebhook: %w", err)
	}

	var runErr error

	select {
	case <-ctx.Done():
	case runErr = <-errCh:
	}

	// Webhook в Telegram не снимаем: при rolling-деплое новый экземпляр уже принимает
	// обновления по тому же адресу.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = fmt.Errorf("webhook shutdown: %w", err)
	}

	return runErr
}

// registerWebhookAndWait регистрирует webhook и ждёт отмены ctx — для случая, когда
// WebhookHandler обслуживает HTTP-сервер самого сервиса.
func (b *Bot) registerWebhookAndWait(ctx context.Context) error {
	cfg := b.cfg.Webhook

	if err := b.api.SetWebhook(ctx, api.SetWebhookRequest{
		URL:                cfg.URL,
		SecretToken:        cfg.Secret,
		AllowedUpdates:     allowedUpdates,
		DropPendingUpdates: cfg.DropPendingUpdates,
	}); err != nil {
		return fmt.Errorf("bot: setWebhook: %w", err)
	}

	<-ctx.Done()

	return nil
}
