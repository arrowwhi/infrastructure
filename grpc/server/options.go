package server

import (
	"net"
	"net/http"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"

	"github.com/arrowwhi/infrastructure/observability/health"
)

// Option настраивает Server.
type Option func(*Server)

// WithServices регистрирует сервисы. Сервисы, реализующие GatewayService,
// дополнительно публикуются через HTTP-шлюз.
func WithServices(services ...Service) Option {
	return func(s *Server) {
		s.services = append(s.services, services...)
	}
}

// WithUnaryInterceptors добавляет unary-интерсепторы после встроенных
// (recovery → logging → metrics → ваши).
func WithUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) Option {
	return func(s *Server) {
		s.unary = append(s.unary, interceptors...)
	}
}

// WithStreamInterceptors добавляет stream-интерсепторы после встроенных.
func WithStreamInterceptors(interceptors ...grpc.StreamServerInterceptor) Option {
	return func(s *Server) {
		s.stream = append(s.stream, interceptors...)
	}
}

// WithServerOptions передаёт произвольные grpc.ServerOption (TLS-креды, keepalive и т.п.).
func WithServerOptions(opts ...grpc.ServerOption) Option {
	return func(s *Server) {
		s.serverOpts = append(s.serverOpts, opts...)
	}
}

// WithGatewayMuxOptions настраивает runtime.ServeMux шлюза (маршалеры, обработчик ошибок и т.п.).
func WithGatewayMuxOptions(opts ...runtime.ServeMuxOption) Option {
	return func(s *Server) {
		s.gwMuxOpts = append(s.gwMuxOpts, opts...)
	}
}

// WithGatewayMiddlewares оборачивает HTTP-обработчик шлюза (CORS, авторизация и т.п.).
// Первая middleware в списке — внешняя.
func WithGatewayMiddlewares(mws ...func(http.Handler) http.Handler) Option {
	return func(s *Server) {
		s.gwMiddlewares = append(s.gwMiddlewares, mws...)
	}
}

// WithListener подменяет листенер (например, bufconn или ":0" в тестах).
func WithListener(lis net.Listener) Option {
	return func(s *Server) {
		s.listener = lis
	}
}

// WithHealthRegistry связывает grpc.health.v1 с общим реестром проверок: реестр
// опрашивается каждые interval, и при падении критичной проверки весь сервер
// (пустое имя сервиса и каждый зарегистрированный сервис) переходит в NOT_SERVING.
// Требует HealthEnabled=true. interval <= 0 — 10 секунд.
func WithHealthRegistry(registry *health.Registry, interval time.Duration) Option {
	return func(s *Server) {
		s.healthRegistry = registry
		s.healthInterval = interval
	}
}
