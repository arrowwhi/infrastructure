package server

import (
	"net"
	"time"
)

// Config — настройки gRPC-сервера и опционального HTTP-шлюза (grpc-gateway).
type Config struct {
	Host string `yaml:"host" env:"GRPC_HOST" env-default:"0.0.0.0"`
	Port string `yaml:"port" env:"GRPC_PORT" env-default:"50051"`

	Gateway GatewayConfig `yaml:"gateway"`

	// MaxRecvMsgSize / MaxSendMsgSize — лимиты размера сообщения в байтах (0 — значение gRPC по умолчанию, 4 МБ).
	MaxRecvMsgSize int `yaml:"max_recv_msg_size" env:"GRPC_MAX_RECV_MSG_SIZE" env-default:"0"`
	MaxSendMsgSize int `yaml:"max_send_msg_size" env:"GRPC_MAX_SEND_MSG_SIZE" env-default:"0"`

	// ShutdownTimeout — сколько ждать завершения активных RPC при остановке,
	// после чего сервер останавливается принудительно.
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env:"GRPC_SHUTDOWN_TIMEOUT" env-default:"30s"`

	ReflectionEnabled bool `yaml:"reflection_enabled" env:"GRPC_REFLECTION_ENABLED" env-default:"true"`
	HealthEnabled     bool `yaml:"health_enabled" env:"GRPC_HEALTH_ENABLED" env-default:"true"`
	// LoggingEnabled — логировать каждый RPC (метод, код, длительность).
	LoggingEnabled bool `yaml:"logging_enabled" env:"GRPC_LOGGING_ENABLED" env-default:"true"`
	// MetricsEnabled — OTel-метрики RPC (observability/metrics). MeterProvider должен быть инициализирован.
	MetricsEnabled bool `yaml:"metrics_enabled" env:"GRPC_METRICS_ENABLED" env-default:"true"`
	// TracingEnabled — спаны на каждый RPC через otelgrpc. TracerProvider должен быть инициализирован.
	TracingEnabled bool `yaml:"tracing_enabled" env:"GRPC_TRACING_ENABLED" env-default:"true"`
}

// GatewayConfig — HTTP/JSON-шлюз поверх gRPC (grpc-gateway).
type GatewayConfig struct {
	Host    string `yaml:"host" env:"GRPC_GATEWAY_HOST" env-default:"0.0.0.0"`
	Port    string `yaml:"port" env:"GRPC_GATEWAY_PORT" env-default:"8081"`
	Enabled bool   `yaml:"enabled" env:"GRPC_GATEWAY_ENABLED" env-default:"false"`

	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout" env:"GRPC_GATEWAY_READ_HEADER_TIMEOUT" env-default:"10s"`
}

// DefaultConfig возвращает конфигурацию по умолчанию (совпадает с env-default).
func DefaultConfig() Config {
	return Config{
		Host:              "0.0.0.0",
		Port:              "50051",
		ShutdownTimeout:   30 * time.Second, //nolint:mnd
		ReflectionEnabled: true,
		HealthEnabled:     true,
		LoggingEnabled:    true,
		MetricsEnabled:    true,
		TracingEnabled:    true,
		Gateway: GatewayConfig{
			Host:              "0.0.0.0",
			Port:              "8081",
			ReadHeaderTimeout: 10 * time.Second, //nolint:mnd
		},
	}
}

// Addr возвращает адрес gRPC-листенера.
func (c Config) Addr() string {
	return net.JoinHostPort(c.Host, c.Port)
}

// Addr возвращает адрес HTTP-шлюза.
func (c GatewayConfig) Addr() string {
	return net.JoinHostPort(c.Host, c.Port)
}
