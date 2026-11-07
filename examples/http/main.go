package main

import (
	"context"

	"github.com/arrowwhi/infrastructure/examples/http/api/openapi"
	"github.com/arrowwhi/infrastructure/examples/http/config"
	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/arrowwhi/infrastructure/observability/metrics"
	"github.com/arrowwhi/infrastructure/observability/tracing"
	"github.com/go-chi/chi/v5/middleware"

	httpserver "github.com/arrowwhi/infrastructure/http/server"
)

func main() {
	// Загрузка конфигурации
	cfg := config.Default()

	// Инициализация глобального логгера
	logger.Init(&cfg.Logger, cfg.ServiceName, cfg.ServiceVersion, cfg.Environment)

	// Инициализация трассировщика
	tp, err := tracing.InitTracer(
		context.Background(),
		cfg.Tracing,
		cfg.ServiceName,
		cfg.ServiceVersion,
		cfg.Environment,
	)
	if err != nil {
		logger.Fatal(context.Background()).Err(err).Msg("Failed to initialize tracer")
	}

	// Инициализация метрик
	mp, err := metrics.InitMeterProvider(
		context.Background(),
		cfg.Metrics,
		cfg.ServiceName,
		cfg.ServiceVersion,
		cfg.Environment,
	)
	if err != nil {
		logger.Fatal(context.Background()).Err(err).Msg("Failed to initialize metrics")
	}

	// Подключение к PostgreSQL. Пример должен запускаться и без базы, поэтому
	// недоступный PostgreSQL здесь не фатален — просто не будет DB-эндпоинтов.
	pool := connectPostgres(context.Background(), cfg, mp)

	// Создание сервера с tracing и metrics middleware
	srv, err := httpserver.New(
		cfg.Server,
		httpserver.WithMiddlewares(
			middleware.Recoverer,
			tracing.Middleware(cfg.ServiceName),
			metrics.Middleware(cfg.ServiceName),
		),
		httpserver.WithShutdownHook(tp.Shutdown),
		httpserver.WithShutdownHook(mp.Shutdown),
	)
	if err != nil {
		logger.Fatal(context.Background()).Err(err).Msg("Failed to create HTTP server")
	}

	if pool != nil {
		defer pool.Close()
	}

	// Создание API server с хендлерами
	apiServ, err := newAPIServer(cfg.ServiceName, pool)
	if err != nil {
		logger.Fatal(context.Background()).Err(err).Msg("Failed to create API server")
	}

	// Регистрация сгенерированных хендлеров
	openapi.HandlerFromMux(apiServ, srv.Router())

	// Регистрация Prometheus метрик на /metrics (если используется Prometheus экспортер)
	if metricsHandler := metrics.Handler(); metricsHandler != nil {
		srv.Router().Handle("GET /metrics", metricsHandler)
		logger.Info(context.Background()).Msg("Prometheus metrics endpoint registered at /metrics")
	}

	// Информируем о pprof эндпоинтах если они включены
	if cfg.Server.PprofEnabled {
		logger.Info(context.Background()).Msg("Pprof endpoints enabled at /debug/pprof/*")
	}

	// Запуск сервера
	logger.Info(context.Background()).Msg("Starting HTTP server...")
	if err := srv.Run(context.Background()); err != nil {
		logger.Fatal(context.Background()).Err(err).Msg("Server error")
	}
}
