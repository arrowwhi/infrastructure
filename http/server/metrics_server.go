package server

import (
	"context"
	"net/http"
	"net/http/pprof"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/arrowwhi/infrastructure/observability/metrics"
)

type metricsServer struct {
	server *http.Server
	router chi.Router
}

func newMetricsServer(config Config) *metricsServer {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	addr := config.Host + ":" + config.MetricsPort

	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	return &metricsServer{
		server: srv,
		router: r,
	}
}

func (m *metricsServer) registerMetricsHandler() {
	if metricsHandler := metrics.Handler(); metricsHandler != nil {
		m.router.Method("GET", "/metrics", metricsHandler)
		logger.Info(context.Background()).Str("path", "/metrics").Msg("Prometheus metrics endpoint registered")
	}
}

func (m *metricsServer) registerPprofHandlers() {
	m.router.Route("/debug/pprof", func(r chi.Router) {
		r.Get("/", pprof.Index)
		r.Get("/cmdline", pprof.Cmdline)
		r.Get("/profile", pprof.Profile)
		r.Get("/symbol", pprof.Symbol)
		r.Get("/trace", pprof.Trace)

		// Обработчик для всех остальных профилей (heap, goroutine, threadcreate, block, mutex, allocs)
		r.Get("/{profile}", func(w http.ResponseWriter, r *http.Request) {
			profile := chi.URLParam(r, "profile")
			pprof.Handler(profile).ServeHTTP(w, r)
		})
	})
}

// ListenAndServe запускает сервер
func (m *metricsServer) ListenAndServe() error {
	logger.Info(context.Background()).Str("addr", m.server.Addr).Msg("Starting metrics server")
	return m.server.ListenAndServe()
}

// Shutdown останавливает сервер gracefully
func (m *metricsServer) Shutdown(ctx context.Context) error {
	logger.Info(ctx).Str("addr", m.server.Addr).Msg("Shutting down metrics server")
	return m.server.Shutdown(ctx)
}
