package telegram

import (
	"context"
	"sync"
	"time"
)

// MemoryStore — хранилище в памяти процесса. Для тестов и локальной разработки:
// при рестарте всё теряется, между копиями бота не разделяется.
type MemoryStore struct {
	items map[StateKey]memoryItem
	now   func() time.Time
	mu    sync.Mutex
}

type memoryItem struct {
	expiresAt time.Time
	state     State
}

// NewMemoryStore создаёт хранилище в памяти.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{items: make(map[StateKey]memoryItem), now: time.Now}
}

// Get возвращает состояние или ErrStateNotFound.
func (m *MemoryStore) Get(_ context.Context, key StateKey) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.items[key]
	if !ok {
		return State{}, ErrStateNotFound
	}

	if !item.expiresAt.IsZero() && !m.now().Before(item.expiresAt) {
		delete(m.items, key)

		return State{}, ErrStateNotFound
	}

	return item.state, nil
}

// Set сохраняет состояние.
func (m *MemoryStore) Set(_ context.Context, key StateKey, s State, ttl time.Duration) error {
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
func (m *MemoryStore) Delete(_ context.Context, key StateKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.items, key)

	return nil
}

// gcLocked время от времени вычищает истёкшие записи, чтобы карта не росла бесконечно.
func (m *MemoryStore) gcLocked() {
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
