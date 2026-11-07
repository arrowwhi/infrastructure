package telegram

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/arrowwhi/infrastructure/observability/logger"
)

const (
	// conflictRetry — пауза при 409 от getUpdates. Короткая и без роста: конфликт
	// снимается, как только старый экземпляр остановится (rolling deploy).
	conflictRetry = 3 * time.Second
	maxBackoff    = 30 * time.Second
)

// runPolling получает обновления через getUpdates до отмены ctx.
func (b *Bot) runPolling(ctx context.Context) error {
	// Выставленный webhook блокирует getUpdates — снимаем его.
	if err := b.api.DeleteWebhook(ctx, false); err != nil {
		logger.Warn(ctx).Err(err).Msg("Failed to delete webhook before polling")
	}

	var offset int64

	backoff := time.Second
	inConflict := false

	for ctx.Err() == nil {
		updates, err := b.api.GetUpdates(ctx, GetUpdatesRequest{
			Offset:         offset,
			Timeout:        int(b.cfg.PollTimeout.Seconds()),
			AllowedUpdates: allowedUpdates,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			var wait time.Duration

			if IsCode(err, http.StatusConflict) {
				// Старый экземпляр ещё опрашивает Telegram — штатно при деплое, пишем один раз.
				if !inConflict {
					logger.Info(ctx).Msg("Another bot instance is polling, waiting for it to stop")

					inConflict = true
				}

				wait = conflictRetry
			} else {
				logger.Warn(ctx).Err(err).Dur("retry_in", backoff).Msg("getUpdates failed")

				wait = backoff
				backoff = min(backoff*2, maxBackoff)
			}

			select {
			case <-ctx.Done():
				return nil
			case <-time.After(wait):
			}

			continue
		}

		backoff = time.Second

		if inConflict {
			logger.Info(ctx).Msg("Polling resumed")

			inConflict = false
		}

		for _, u := range updates {
			if err = b.dispatcher.enqueue(ctx, u); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, errStopped) {
					// Обновление не подтверждено (offset не сдвинут) — Telegram пришлёт его снова.
					return nil
				}

				return err
			}

			offset = u.UpdateID + 1
		}
	}

	return nil
}
