package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// ErrStateNotFound возвращается Get, когда состояния нет или оно истекло.
var ErrStateNotFound = errors.New("state: not found")

// StateKey — владелец состояния. В личном чате ChatID == UserID; в группе состояние
// хранится отдельно для каждого участника.
type StateKey struct {
	ChatID int64
	UserID int64
}

// String возвращает ключ в виде "chat:user".
func (k StateKey) String() string {
	return strconv.FormatInt(k.ChatID, 10) + ":" + strconv.FormatInt(k.UserID, 10)
}

// State — шаг диалога и его данные.
type State struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data,omitempty"`
}

// NewState создаёт состояние с данными data (сериализуются в JSON; nil — без данных).
func NewState(name string, data any) (State, error) {
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
	Get(ctx context.Context, key StateKey) (State, error)
	Set(ctx context.Context, key StateKey, s State, ttl time.Duration) error
	Delete(ctx context.Context, key StateKey) error
}
