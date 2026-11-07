// Package state stores per-chat dialog state for Telegram bots.
//
// Состояние — это имя шага диалога (например, "awaiting_rating") и произвольные данные
// в JSON (выбранный фильм, показанные кандидаты). Бот читает его при каждом
// обновлении, чтобы понять, к чему относится очередное сообщение или нажатие кнопки.
//
// Реализации:
//   - NewMemory — в памяти процесса: тесты и локальная разработка, теряется при рестарте;
//   - NewPostgres — таблица в PostgreSQL: переживает рестарты, работает с несколькими копиями бота;
//   - NewRedis — ключи Redis с TTL: то же, но устаревшие записи удаляет сам Redis.
package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// ErrNotFound возвращается Get, когда состояния нет или оно истекло.
var ErrNotFound = errors.New("state: not found")

// Key — владелец состояния. В личном чате ChatID == UserID; в группе состояние
// хранится отдельно для каждого участника.
type Key struct {
	ChatID int64
	UserID int64
}

// String возвращает ключ в виде "chat:user".
func (k Key) String() string {
	return strconv.FormatInt(k.ChatID, 10) + ":" + strconv.FormatInt(k.UserID, 10)
}

// State — шаг диалога и его данные.
type State struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data,omitempty"`
}

// New создаёт состояние с данными data (сериализуются в JSON; nil — без данных).
func New(name string, data any) (State, error) {
	s := State{Name: name}

	if data == nil {
		return s, nil
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return State{}, fmt.Errorf("state: encode data: %w", err)
	}

	s.Data = raw

	return s, nil
}

// Decode разбирает данные состояния в v.
func (s State) Decode(v any) error {
	if len(s.Data) == 0 {
		return nil
	}

	if err := json.Unmarshal(s.Data, v); err != nil {
		return fmt.Errorf("state: decode data: %w", err)
	}

	return nil
}

// Store — хранилище состояний. ttl <= 0 означает «без срока жизни».
type Store interface {
	Get(ctx context.Context, key Key) (State, error)
	Set(ctx context.Context, key Key, s State, ttl time.Duration) error
	Delete(ctx context.Context, key Key) error
}
