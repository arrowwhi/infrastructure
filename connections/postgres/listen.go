package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/arrowwhi/infrastructure/observability/logger"
)

const (
	defaultNotifyBuffer   = 64
	defaultReconnectDelay = time.Second
)

// Listener — подписка на уведомления PostgreSQL (LISTEN/NOTIFY).
//
// Работает на выделенном соединении вне пула: сессия, ожидающая уведомлений, занята
// постоянно, и держать её в пуле нельзя. При обрыве связи соединение переустанавливается
// и подписки восстанавливаются автоматически.
//
// Внимание: подписка живёт ровно столько, сколько существует её соединение с PostgreSQL.
// Если между сервисом и базой стоит промежуточный слой, переиспользующий соединения между
// клиентами, уведомления до подписчика не дойдут — нужно прямое подключение.
type Listener struct {
	notifications chan *pgconn.Notification
	cancel        context.CancelFunc
	done          chan struct{}
	connConfig    *pgx.ConnConfig
	channels      []string
	reconnect     time.Duration
}

// Listen подписывается на каналы уведомлений.
// Слушатель работает, пока не будет вызван Close или отменён переданный контекст.
func (p *Pool) Listen(ctx context.Context, channels ...string) (*Listener, error) {
	if p.primary == nil {
		return nil, ErrClosed
	}

	if len(channels) == 0 {
		return nil, fmt.Errorf("%w: no channels to listen", ErrInvalidConfig)
	}

	listenCtx, cancel := context.WithCancel(ctx)

	listener := &Listener{
		connConfig:    p.primary.Config().ConnConfig.Copy(),
		channels:      channels,
		notifications: make(chan *pgconn.Notification, defaultNotifyBuffer),
		cancel:        cancel,
		done:          make(chan struct{}),
		reconnect:     defaultReconnectDelay,
	}

	// Уведомления приходят вне цикла «запрос — ответ», трейсер здесь только мешает.
	listener.connConfig.Tracer = nil

	go listener.run(listenCtx)

	return listener, nil
}

// Notify отправляет уведомление в канал.
func (p *Pool) Notify(ctx context.Context, channel, payload string) error {
	if _, err := p.Exec(ctx, "SELECT pg_notify($1, $2)", channel, payload); err != nil {
		return fmt.Errorf("notify %q: %w", channel, err)
	}

	return nil
}

// Notifications возвращает канал уведомлений. Он закрывается после завершения работы
// слушателя, поэтому чтение из него в for-range корректно завершится само.
func (l *Listener) Notifications() <-chan *pgconn.Notification {
	return l.notifications
}

// Close останавливает слушателя и дожидается освобождения соединения.
func (l *Listener) Close() {
	l.cancel()
	<-l.done
}

// run держит соединение и переустанавливает его при обрывах.
func (l *Listener) run(ctx context.Context) {
	defer close(l.done)
	defer close(l.notifications)

	for ctx.Err() == nil {
		if err := l.listenOnce(ctx); err != nil && ctx.Err() == nil {
			logger.Error(ctx).
				Err(err).
				Strs("channels", l.channels).
				Dur("retry_in", l.reconnect).
				Msg("postgres: listener connection lost, reconnecting")
		}

		if ctx.Err() != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(l.reconnect):
		}
	}
}

// listenOnce устанавливает соединение, подписывается и читает уведомления
// до первой ошибки.
func (l *Listener) listenOnce(ctx context.Context) error {
	conn, err := pgx.ConnectConfig(ctx, l.connConfig)
	if err != nil {
		return fmt.Errorf("connect listener: %w", err)
	}

	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()

		_ = conn.Close(closeCtx)
	}()

	for _, channel := range l.channels {
		// Имя канала — идентификатор, параметром его передать нельзя, поэтому экранируем.
		if _, err = conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
			return fmt.Errorf("listen %q: %w", channel, err)
		}
	}

	for {
		notification, waitErr := conn.WaitForNotification(ctx)
		if waitErr != nil {
			return fmt.Errorf("wait for notification: %w", waitErr)
		}

		select {
		case l.notifications <- notification:
		case <-ctx.Done():
			return nil
		}
	}
}
