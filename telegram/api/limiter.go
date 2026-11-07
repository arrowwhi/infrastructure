package api

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// chatLimiterTTL — сколько хранить лимитер чата без активности.
const chatLimiterTTL = 10 * time.Minute

// limiter реализует лимиты Telegram: общий на бота и отдельный на каждый чат.
// Без них массовая рассылка быстро упирается в 429 и бан на десятки секунд.
type limiter struct {
	global   *rate.Limiter
	chats    map[int64]*chatLimiter
	cfg      Config
	lastGC   time.Time
	mu       sync.Mutex
	disabled bool
}

type chatLimiter struct {
	lim      *rate.Limiter
	lastUsed time.Time
}

func newLimiter(cfg Config) *limiter {
	l := &limiter{cfg: cfg, chats: make(map[int64]*chatLimiter), lastGC: time.Now()}

	if cfg.GlobalRate <= 0 {
		l.disabled = true

		return l
	}

	l.global = rate.NewLimiter(rate.Limit(cfg.GlobalRate), int(cfg.GlobalRate))

	return l
}

// wait блокируется, пока лимиты не разрешат запрос. Возвращает время ожидания.
func (l *limiter) wait(ctx context.Context, chatID int64) (time.Duration, error) {
	if l.disabled {
		return 0, nil
	}

	start := time.Now()

	if chatID != 0 {
		if err := l.forChat(chatID).Wait(ctx); err != nil {
			return time.Since(start), err
		}
	}

	if err := l.global.Wait(ctx); err != nil {
		return time.Since(start), err
	}

	return time.Since(start), nil
}

func (l *limiter) forChat(chatID int64) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	if now.Sub(l.lastGC) > chatLimiterTTL {
		for id, cl := range l.chats {
			if now.Sub(cl.lastUsed) > chatLimiterTTL {
				delete(l.chats, id)
			}
		}

		l.lastGC = now
	}

	cl, ok := l.chats[chatID]
	if !ok {
		interval := l.cfg.ChatInterval
		if chatID < 0 { // группы и каналы
			interval = l.cfg.GroupInterval
		}

		burst := l.cfg.ChatBurst
		if burst <= 0 {
			burst = 1
		}

		limit := rate.Inf
		if interval > 0 {
			limit = rate.Every(interval)
		}

		cl = &chatLimiter{lim: rate.NewLimiter(limit, burst)}
		l.chats[chatID] = cl
	}

	cl.lastUsed = now

	return cl.lim
}
