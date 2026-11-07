// Package health provides a registry of dependency checks shared by HTTP readiness probes,
// the gRPC health service and metrics.
//
// Проверки регистрируются один раз (postgres.Pool.Healthcheck, redis.Client.Healthcheck,
// произвольные функции), а дальше один и тот же результат отдают:
//   - HTTP: LivenessHandler (/healthz) и Registry.ReadinessHandler (/ready);
//   - gRPC: grpc/server.WithHealthRegistry переключает grpc.health.v1 в NOT_SERVING;
//   - метрики: Registry.RegisterMetrics публикует health.check.status и health.check.duration.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

// DefaultTimeout — таймаут одной проверки по умолчанию. Проба Kubernetes обычно ждёт
// 1–5 секунд, поэтому проверка не должна висеть дольше.
const DefaultTimeout = 2 * time.Second

// CheckFunc проверяет одну зависимость. Должна уважать отмену ctx.
type CheckFunc func(ctx context.Context) error

// Status — итог проверки.
type Status string

const (
	// StatusOK — все проверки прошли.
	StatusOK Status = "ok"
	// StatusDegraded — упали только некритичные проверки; сервис готов принимать трафик.
	StatusDegraded Status = "degraded"
	// StatusFail — упала хотя бы одна критичная проверка.
	StatusFail Status = "fail"
)

// CheckResult — результат одной проверки.
type CheckResult struct {
	Status   Status        `json:"status"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration_ns"`
	Critical bool          `json:"critical"`
}

// Report — результат всех проверок.
type Report struct {
	Checks    map[string]CheckResult `json:"checks,omitempty"`
	CheckedAt time.Time              `json:"checked_at"`
	Status    Status                 `json:"status"`
}

// Ready сообщает, можно ли направлять трафик на сервис.
func (r Report) Ready() bool {
	return r.Status != StatusFail
}

type check struct {
	fn       CheckFunc
	name     string
	timeout  time.Duration
	critical bool
}

// CheckOption настраивает отдельную проверку.
type CheckOption func(*check)

// NonCritical помечает проверку как некритичную: её падение даёт StatusDegraded,
// но не снимает сервис с трафика (например, кэш, без которого можно работать медленнее).
func NonCritical() CheckOption {
	return func(c *check) { c.critical = false }
}

// WithCheckTimeout задаёт таймаут конкретной проверки.
func WithCheckTimeout(d time.Duration) CheckOption {
	return func(c *check) { c.timeout = d }
}

// Option настраивает Registry.
type Option func(*Registry)

// WithTimeout задаёт таймаут проверок по умолчанию.
func WithTimeout(d time.Duration) Option {
	return func(r *Registry) { r.timeout = d }
}

// Registry хранит проверки и последний отчёт. Безопасен для конкурентного использования.
type Registry struct {
	last    Report
	checks  []check
	timeout time.Duration
	mu      sync.RWMutex
}

// New создаёт пустой реестр. Без проверок сервис считается готовым.
func New(opts ...Option) *Registry {
	r := &Registry{timeout: DefaultTimeout}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Register добавляет проверку. Повторная регистрация с тем же именем заменяет прежнюю.
func (r *Registry) Register(name string, fn CheckFunc, opts ...CheckOption) {
	c := check{name: name, fn: fn, critical: true}

	for _, opt := range opts {
		opt(&c)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.checks {
		if r.checks[i].name == name {
			r.checks[i] = c

			return
		}
	}

	r.checks = append(r.checks, c)
}

// Check выполняет все проверки параллельно, каждую со своим таймаутом, и запоминает отчёт.
func (r *Registry) Check(ctx context.Context) Report {
	r.mu.RLock()
	checks := append([]check(nil), r.checks...)
	defaultTimeout := r.timeout
	r.mu.RUnlock()

	results := make(map[string]CheckResult, len(checks))

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		now = time.Now()
	)

	for _, c := range checks {
		wg.Add(1)

		go func(c check) {
			defer wg.Done()

			res := runCheck(ctx, c, defaultTimeout)

			mu.Lock()
			results[c.name] = res
			mu.Unlock()
		}(c)
	}

	wg.Wait()

	report := Report{Status: StatusOK, Checks: results, CheckedAt: now}

	for _, res := range results {
		switch {
		case res.Status == StatusOK:
		case res.Critical:
			report.Status = StatusFail
		case report.Status == StatusOK:
			report.Status = StatusDegraded
		}
	}

	r.mu.Lock()
	r.last = report
	r.mu.Unlock()

	return report
}

// Last возвращает последний отчёт (нулевой, если Check ещё не вызывался).
func (r *Registry) Last() Report {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.last
}

// Names возвращает имена зарегистрированных проверок по алфавиту.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.checks))
	for _, c := range r.checks {
		names = append(names, c.name)
	}

	sort.Strings(names)

	return names
}

// Watch периодически выполняет проверки и передаёт отчёт в fn, пока не отменён ctx.
// Первая проверка выполняется сразу.
func (r *Registry) Watch(ctx context.Context, interval time.Duration, fn func(Report)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		fn(r.Check(ctx))

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runCheck(ctx context.Context, c check, defaultTimeout time.Duration) (res CheckResult) {
	timeout := c.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	res.Critical = c.critical

	defer func() {
		if p := recover(); p != nil {
			res.Status = StatusFail
			res.Error = fmt.Sprintf("panic: %v", p)
		}

		res.Duration = time.Since(start)
	}()

	err := c.fn(ctx)
	if err == nil && ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		err = fmt.Errorf("timeout after %s", timeout)
	}

	if err != nil {
		res.Status = StatusFail
		res.Error = err.Error()

		return res
	}

	res.Status = StatusOK

	return res
}

// LivenessHandler отвечает 200, пока процесс жив. Зависимости здесь намеренно не
// проверяются: иначе падение базы приводит к рестарту всех подов, а не к снятию с трафика.
func LivenessHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]Status{"status": StatusOK})
	})
}

// ReadinessHandler выполняет проверки и отвечает 200 (ok/degraded) или 503 (fail) с отчётом.
func (r *Registry) ReadinessHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		report := r.Check(req.Context())

		code := http.StatusOK
		if !report.Ready() {
			code = http.StatusServiceUnavailable
		}

		writeJSON(w, code, report)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)

	_ = json.NewEncoder(w).Encode(v)
}
