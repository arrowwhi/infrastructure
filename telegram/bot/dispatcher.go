package bot

import (
	"context"
	"errors"
	"sync"

	"github.com/arrowwhi/infrastructure/telegram/api"
)

// errStopped возвращается enqueue после остановки диспетчера.
var errStopped = errors.New("bot: dispatcher stopped")

// dispatcher раздаёт обновления воркерам. Чат всегда попадает к одному и тому же
// воркеру (chat_id mod N), поэтому сообщения одного чата обрабатываются строго
// по порядку, а разные чаты — параллельно.
type dispatcher struct {
	process func(api.Update)
	queues  []chan api.Update
	wg      sync.WaitGroup
	mu      sync.RWMutex
	started bool
	stopped bool
}

func newDispatcher(workers, queueSize int, process func(api.Update)) *dispatcher {
	d := &dispatcher{process: process, queues: make([]chan api.Update, workers)}

	for i := range d.queues {
		d.queues[i] = make(chan api.Update, queueSize)
	}

	return d
}

func (d *dispatcher) start() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.started {
		return
	}

	d.started = true

	for _, q := range d.queues {
		d.wg.Add(1)

		go func(q chan api.Update) {
			defer d.wg.Done()

			for u := range q {
				d.process(u)
			}
		}(q)
	}
}

// enqueue кладёт обновление в очередь его чата; блокируется, пока очередь полна.
func (d *dispatcher) enqueue(ctx context.Context, u api.Update) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.stopped {
		return errStopped
	}

	q := d.queues[d.index(u)]

	select {
	case q <- u:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stop перестаёт принимать обновления и ждёт, пока воркеры разберут очереди.
func (d *dispatcher) stop() {
	d.mu.Lock()

	if d.stopped {
		d.mu.Unlock()

		return
	}

	d.stopped = true

	for _, q := range d.queues {
		close(q)
	}

	d.mu.Unlock()
	d.wg.Wait()
}

// size возвращает суммарную длину очередей.
func (d *dispatcher) size() int {
	n := 0
	for _, q := range d.queues {
		n += len(q)
	}

	return n
}

func (d *dispatcher) index(u api.Update) int {
	var key int64
	if chat := u.Chat(); chat != nil {
		key = chat.ID
	}

	if key < 0 {
		key = -key
	}

	return int(key % int64(len(d.queues)))
}
