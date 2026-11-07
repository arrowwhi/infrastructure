package server

import (
	"context"
	"fmt"

	"github.com/arrowwhi/infrastructure/observability/health"
)

// HealthChecker interface for custom health checks.
//
// Deprecated: используйте AddHealthCheckFunc или Health().Register — они принимают
// контекст, поэтому проверка уважает таймаут пробы.
type HealthChecker interface {
	Check() error
}

func (s *Server) registerHealthHandlers() {
	if s.health == nil {
		s.health = health.New()
	}

	s.router.Method("GET", "/healthz", health.LivenessHandler())
	s.router.Method("GET", "/ready", s.health.ReadinessHandler())
}

// Health возвращает реестр проверок, который отдаёт /ready. Его же можно передать
// в gRPC-сервер (grpc/server.WithHealthRegistry) и зарегистрировать в метриках.
func (s *Server) Health() *health.Registry {
	return s.health
}

// AddHealthCheckFunc регистрирует проверку зависимости для /ready.
func (s *Server) AddHealthCheckFunc(name string, fn health.CheckFunc, opts ...health.CheckOption) {
	s.health.Register(name, fn, opts...)
}

// AddHealthCheck регистрирует проверку старого вида (без контекста).
//
// Deprecated: используйте AddHealthCheckFunc.
func (s *Server) AddHealthCheck(checker HealthChecker) {
	s.health.Register(fmt.Sprintf("%T", checker), func(context.Context) error {
		return checker.Check()
	})
}
