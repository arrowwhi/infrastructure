package state

import (
	"context"
	"sync"
	"time"
)

// Memory — хранилище в памяти процесса. Для тестов и локальной разработки:
// при рестарте всё теряется, между копиями бота не разделяется.
type Memory struct {
	items map[Key]memoryItem
	now   func() time.Time
	mu    sync.Mutex
}

type memoryItem struct {
	expiresAt time.Time
	state     State
}

// NewMemory создаёт хранилище в памяти.
func NewMemory() *Memory {
	return &Memory{items: make(map[Key]memoryItem), now: time.Now}
}

// Get возвращает состояние или ErrNotFound.
func (m *Memory) Get(_ context.Context, key Key) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.items[key]
	if !ok {
		return State{}, ErrNotFound
	}

	if !item.expiresAt.IsZero() && !m.now().Before(item.expiresAt) {
		delete(m.items, key)

		return State{}, ErrNotFound
	}

	return item.state, nil
}

// Set сохраняет состояние.
func (m *Memory) Set(_ context.Context, key Key, s State, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	item := memoryItem{state: s}
	if ttl > 0 {
		item.expiresAt = m.now().Add(ttl)
	}

	m.items[key] = item
	m.gcLocked()

	return nil
}

// Delete удаляет состояние.
func (m *Memory) Delete(_ context.Context, key Key) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.items, key)

	return nil
}

// gcLocked время от времени вычищает истёкшие записи, чтобы карта не росла бесконечно.
func (m *Memory) gcLocked() {
	const gcThreshold = 1024
	if len(m.items) < gcThreshold {
		return
	}

	now := m.now()

	for k, item := range m.items {
		if !item.expiresAt.IsZero() && !now.Before(item.expiresAt) {
			delete(m.items, k)
		}
	}
}
