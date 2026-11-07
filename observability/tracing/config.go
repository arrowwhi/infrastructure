package tracing

import "time"

const (
	// defaultShutdownTimeoutSeconds is the default timeout for graceful shutdown in seconds
	defaultShutdownTimeoutSeconds = 5
)

type Config struct {

	// Exporter тип экспортера: otlp-grpc, otlp-http, stdout (stdout — только для отладки:
	// печатает каждый спан целиком).
	Exporter string `yaml:"exporter" env:"OTEL_EXPORTER" env-default:"otlp-grpc"`

	// OTLP endpoint (host:port) для otlp-grpc (обычно :4317) или otlp-http (обычно :4318)
	OTLPEndpoint string `yaml:"otlp_endpoint" env:"OTEL_EXPORTER_OTLP_ENDPOINT" env-default:"localhost:4317"`

	// SamplingRate - доля корневых трейсов для сэмплирования (0.0 - 1.0). Если у входящего
	// запроса уже есть родительский спан, решение родителя сохраняется (ParentBased).
	SamplingRate float64 `yaml:"sampling_rate" env:"OTEL_SAMPLING_RATE" env-default:"1.0"`

	// ShutdownTimeout - таймаут для graceful shutdown
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env:"SHUTDOWN_TIMEOUT" env-default:"5s"`
	// Enabled - включена ли трассировка. По умолчанию выключена: без коллектора экспортёр
	// только шумит ошибками в логах.
	Enabled bool `yaml:"enabled" env:"OTEL_ENABLED" env-default:"false"`

	// OTLP insecure - использовать ли незащищенное соединение
	OTLPInsecure bool `yaml:"otlp_insecure" env:"OTEL_EXPORTER_OTLP_INSECURE" env-default:"true"`
}

func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		Exporter:        "otlp-grpc",
		OTLPEndpoint:    "localhost:4317",
		OTLPInsecure:    true,
		SamplingRate:    1.0,
		ShutdownTimeout: defaultShutdownTimeoutSeconds * time.Second,
	}
}
