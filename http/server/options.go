package server

import (
	"context"
	"net/http"
)

// Option функция для конфигурации сервера
type Option func(*Server)

// WithMiddleware добавляет одну middleware в сервер
func WithMiddleware(mw func(http.Handler) http.Handler) Option {
	return func(s *Server) {
		s.router.Use(mw)
	}
}

// WithMiddlewares добавляет несколько middleware в сервер
func WithMiddlewares(mws ...func(http.Handler) http.Handler) Option {
	return func(s *Server) {
		for _, mw := range mws {
			s.router.Use(mw)
		}
	}
}

// WithShutdownHook добавляет функцию для вызова при graceful shutdown
func WithShutdownHook(fn func(context.Context) error) Option {
	return func(s *Server) {
		s.shutdown = append(s.shutdown, fn)
	}
}
