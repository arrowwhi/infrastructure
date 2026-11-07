package client

import (
	"time"
)

type Config struct {
	TLSConfig *TLSConfig       `yaml:"tls_config" env-default:"{}"`
	Retry     *RetryConfig     `yaml:"retry" env-default:"{}"`
	KeepAlive *KeepAliveConfig `yaml:"keep_alive" env-default:"{}"`
	Endpoints string           `yaml:"endpoints" env-default:"localhost:50051"`
	// Timeout — дедлайн вызова по умолчанию, если в контексте вызова дедлайна нет (0 — без дедлайна).
	Timeout         time.Duration `yaml:"timeout" env-default:"30s"`
	EnableLogging   bool          `yaml:"enable_logging" env-default:"true"`
	EnableTLS       bool          `yaml:"enable_tls" env-default:"false"`
	EnableRetries   bool          `yaml:"enable_retries" env-default:"true"`
	EnableKeepAlive bool          `yaml:"enable_keep_alive" env-default:"true"`
	// EnableMetrics — OTel-метрики grpc.client.* (MeterProvider должен быть инициализирован).
	EnableMetrics bool `yaml:"enable_metrics" env-default:"true"`
	// EnableTracing — спаны на каждый вызов через otelgrpc и проброс trace context на сервер.
	EnableTracing bool `yaml:"enable_tracing" env-default:"true"`
	// ServiceName — имя для meter'а; если пусто, используется "grpc-client".
	ServiceName string `yaml:"service_name"`
}

type TLSConfig struct {
	CertFile string `yaml:"cert_file" env-default:""`
	KeyFile  string `yaml:"key_file" env-default:""`
	CAFile   string `yaml:"ca_file" env-default:""`
}

type KeepAliveConfig struct {
	RefreshTime         time.Duration `yaml:"time" env-default:"30s"`
	Timeout             time.Duration `yaml:"timeout" env-default:"5s"`
	PermitWithoutStream bool          `yaml:"permit_without_stream" env-default:"true"`
}

type RetryConfig struct {
	// MaxRetries — число повторов после первой попытки (всего попыток MaxRetries+1).
	MaxRetries uint `yaml:"max_retries" env-default:"3"`
	// PerRetryTimeout — дедлайн одной попытки (0 — общий дедлайн вызова).
	PerRetryTimeout time.Duration `yaml:"per_retry_timeout" env-default:"0s"`
	BackoffTime     time.Duration `yaml:"backoff" env-default:"100ms"`
	BackoffFraction float64       `yaml:"backoff_fraction" env-default:"0.3"`
}
