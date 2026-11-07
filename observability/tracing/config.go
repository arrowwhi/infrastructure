package tracing

import "time"

const (
	// defaultShutdownTimeoutSeconds is the default timeout for graceful shutdown in seconds
	defaultShutdownTimeoutSeconds = 5
)

type Config struct {

	// Exporter тип экспортера: stdout, otlp-grpc, otlp-http
	Exporter string `yaml:"exporter" env:"OTEL_EXPORTER" env-default:"stdout"`

	// OTLP endpoint для otlp-grpc или otlp-http
	OTLPEndpoint string `yaml:"otlp_endpoint" env:"OTEL_EXPORTER_OTLP_ENDPOINT" env-default:"localhost:4317"`

	// SamplingRate - процент трейсов для сэмплирования (0.0 - 1.0)
	SamplingRate float64 `yaml:"sampling_rate" env:"OTEL_SAMPLING_RATE" env-default:"1.0"`

	// ShutdownTimeout - таймаут для graceful shutdown
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env:"SHUTDOWN_TIMEOUT" env-default:"5s"`
	// Enabled - включена ли трассировка
	Enabled bool `yaml:"enabled" env:"OTEL_ENABLED" env-default:"true"`

	// OTLP insecure - использовать ли незащищенное соединение
	OTLPInsecure bool `yaml:"otlp_insecure" env:"OTEL_EXPORTER_OTLP_INSECURE" env-default:"true"`
}

func DefaultConfig() Config {
	return Config{
		Enabled:         true,
		Exporter:        "stdout",
		OTLPEndpoint:    "localhost:4317",
		OTLPInsecure:    true,
		SamplingRate:    1.0,
		ShutdownTimeout: defaultShutdownTimeoutSeconds * time.Second,
	}
}
