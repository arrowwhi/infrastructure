// Package server provides an HTTP/HTTP3 server implementation with chi router, TLS, and mTLS support.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/arrowwhi/infrastructure/http/server/middlewares"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/arrowwhi/infrastructure/observability/health"
	"github.com/arrowwhi/infrastructure/observability/logger"
)

type Server struct {
	router        chi.Router
	strategy      serverStrategy
	health        *health.Registry
	shutdown      []func(context.Context) error
	config        Config
	mu            sync.Mutex
	metricsServer *metricsServer
}

func New(config Config, opts ...Option) (*Server, error) {
	r := chi.NewRouter()

	// Default middleware
	r.Use(middleware.Recoverer)
	r.Use(middlewares.RequestID)
	r.Use(middlewares.RealIP)
	r.Use(middlewares.Log)

	s := &Server{
		config:   config,
		router:   r,
		shutdown: make([]func(context.Context) error, 0),
	}

	// Выбираем стратегию сервера (HTTP или HTTP/3)
	server, err := choiceStrategy(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create server strategy: %w", err)
	}
	s.strategy = server

	// Устанавливаем роутер в стратегию
	s.strategy.SetHandler(r)

	// Применяем опции
	for _, opt := range opts {
		opt(s)
	}

	s.registerHealthHandlers()

	// Регистрируем Swagger документацию если включена
	if config.IsSwaggerEnabled {
		s.RegisterDocsHandler()
	}

	if config.MetricsEnabled || config.PprofEnabled {
		s.metricsServer = newMetricsServer(config)
		if config.MetricsEnabled {
			s.metricsServer.registerMetricsHandler()
		}

		if config.PprofEnabled {
			s.metricsServer.registerPprofHandlers()
		}
	}

	return s, nil
}

// Router возвращает chi.Router для регистрации хендлеров
func (s *Server) Router() chi.Router {
	return s.router.Group(nil)
}

// Run запускает сервер с graceful shutdown (блокирующий)
func (s *Server) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, gCtx := errgroup.WithContext(runCtx)

	g.Go(func() error {
		logger.Info(gCtx).Str("addr", s.config.Addr()).Msg("Starting main server")
		err := s.strategy.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("main server error: %w", err)
		}
		return nil
	})

	if s.metricsServer != nil {
		g.Go(func() error {
			err := s.metricsServer.ListenAndServe()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("metrics server error: %w", err)
			}
			return nil
		})
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	serverErrors := make(chan error, 1)

	go func() {
		if err := g.Wait(); err != nil {
			serverErrors <- err
		}
		close(serverErrors)
	}()

	select {
	case err := <-serverErrors:
		if err != nil {
			logger.Error(ctx).Err(err).Msg("Server error occurred")
			cancel()
			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
			defer cancelShutdown()
			if shutdownErr := s.Shutdown(shutdownCtx); shutdownErr != nil {
				return fmt.Errorf("shutdown after error failed: %w (original error: %v)", shutdownErr, err)
			}
			return err
		}

	case sig := <-shutdown:
		logger.Info(ctx).Str("signal", sig.String()).Msg("Received shutdown signal")
		cancel()
		if err := s.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown error: %w", err)
		}

	case <-ctx.Done():
		logger.Info(ctx).Msg("Context cancelled, shutting down")
		cancel()
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
		defer cancelShutdown()
		if err := s.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown error: %w", err)
		}
	}

	return nil
}

// Shutdown останавливает сервер gracefully
func (s *Server) Shutdown(ctx context.Context) error {

	shutdownCtx, cancel := context.WithTimeout(ctx, s.config.ShutdownTimeout)
	defer cancel()

	logger.Info(shutdownCtx).Msg("Shutting down HTTP server")

	shutdownErr := s.shutdownHTTPServer(shutdownCtx)

	s.mu.Lock()
	hooks := s.shutdown
	s.mu.Unlock()

	for i, hook := range hooks {
		if err := hook(shutdownCtx); err != nil {
			logger.Error(shutdownCtx).
				Err(err).
				Int("hook_index", i).
				Msg("Shutdown hook failed")
		}
	}

	logger.Info(shutdownCtx).Msg("HTTP server stopped")

	return shutdownErr
}

func (s *Server) shutdownHTTPServer(ctx context.Context) error {

	if s.metricsServer != nil {
		if err := s.metricsServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown metrics server: %w", err)
		}
	}

	if err := s.strategy.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown main server: %w", err)
	}

	return nil
}

// Handler возвращает http.Handler для тестирования сервера
func (s *Server) Handler() http.Handler {
	return s.router
}
