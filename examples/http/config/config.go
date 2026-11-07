// Package config provides configuration structures for the HTTP server example.
package config

import (
	"time"

	"github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/arrowwhi/infrastructure/observability/metrics"
	"github.com/arrowwhi/infrastructure/observability/tracing"

	httpserver "github.com/arrowwhi/infrastructure/http/server"
)

// Config содержит все конфигурации для HTTP сервера примера
type Config struct {

	// Метаданные для сервиса
	ServiceName    string
	ServiceVersion string
	Environment    string
	Logger         logger.Config
	Metrics        metrics.Config
	Tracing        tracing.Config
	Server         httpserver.Config
	Postgres       postgres.Config
}

// Default возвращает конфигурацию по умолчанию
func Default() Config {
	return Config{
		Server: httpserver.Config{
			Host:         "localhost",
			Port:         "8080",
			MetricsPort:  "9091",
			HTTP3Enabled: false,
			TLS: httpserver.TLSConfig{
				Enabled:     false,
				MTLSEnabled: false,
				// Для локального TLS сгенерируйте сертификаты: make create-server-sert
				CertFile: "examples/local_crt/server-cert.pem",
				KeyFile:  "examples/local_crt/server-key.pem",
				CAFile:   "examples/local_crt/ca-cert.pem",
			},
			IsSwaggerEnabled: true,
			Docs: httpserver.DocsConfig{
				URL:      "/openapi.yaml",
				Path:     "/docs",
				SpecFile: "examples/http/api/openapi/openapi.yaml",
			},
			MetricsEnabled:    true,
			PprofEnabled:      true,
			ReadTimeout:       15 * time.Second, //nolint:mnd
			WriteTimeout:      15 * time.Second, //nolint:mnd
			IdleTimeout:       60 * time.Second, //nolint:mnd
			ReadHeaderTimeout: 10 * time.Second, //nolint:mnd
			ShutdownTimeout:   30 * time.Second, //nolint:mnd
		},
		Logger: logger.Config{
			Level:        "debug",
			Stacktrace:   true,
			PrettyOutput: false,
			Masking:      true,
		},
		Metrics: metrics.Config{
			Enabled:        true,
			Exporter:       "otlp-grpc",
			OTLPEndpoint:   "localhost:4317",
			OTLPInsecure:   true,
			MetricInterval: 10, //nolint:mnd
		},
		Tracing: tracing.Config{
			Enabled:      true,
			Exporter:     "otlp-grpc",
			OTLPEndpoint: "localhost:4317",
			OTLPInsecure: true,
			SamplingRate: 1.0,
		},
		Postgres:       defaultPostgres(),
		ServiceName:    "http-server-example",
		ServiceVersion: "1.0.0",
		Environment:    "development",
	}
}

// defaultPostgres — подключение к базе из examples/docker-compose.yml.
//
// Инструментация здесь включена явно: в самом коннекторе она выключена по умолчанию,
// а пример существует ровно для того, чтобы показать её в Jaeger и Prometheus.
func defaultPostgres() postgres.Config {
	cfg := postgres.DefaultConfig()

	cfg.Host = "localhost"
	cfg.Port = 5432
	cfg.User = "example"
	cfg.Password = "example"
	cfg.Database = "example"
	cfg.ApplicationName = "http-server-example"

	cfg.TracingEnabled = true
	cfg.MetricsEnabled = true
	cfg.SlowQueryLog = true
	cfg.SlowQueryThreshold = 100 * time.Millisecond

	// Серверный таймаут: отмена по context не гарантирует, что PostgreSQL
	// действительно прекратит выполнение запроса.
	cfg.StatementTimeout = 30 * time.Second

	return cfg
}
