package server

import "time"

// TLSConfig содержит настройки TLS/mTLS
type TLSConfig struct {
	CertFile string `yaml:"cert_file" env:"HTTP_TLS_CERT_FILE"`
	KeyFile  string `yaml:"key_file" env:"HTTP_TLS_KEY_FILE"`
	CAFile   string `yaml:"ca_file" env:"HTTP_TLS_CA_FILE"`

	// Сертификаты в виде байтовых массивов (приоритетнее, чем файлы)
	CertData []byte `yaml:"cert_data" env:"TLS_CERT_DATA"`
	KeyData  []byte `yaml:"key_data" env:"TLS_KEY_DATA"`
	CAData   []byte `yaml:"ca_data" env:"TLS_CA_DATA"`

	Enabled       bool `yaml:"enabled" env:"HTTP_TLS_ENABLED" env-default:"false"`
	MTLSEnabled   bool `yaml:"mtls_enabled" env:"HTTP_MTLS_ENABLED" env-default:"false"`
	Base64Encoded bool `yaml:"base64_encoded" env-default:"false"`
}

type Config struct {
	Docs DocsConfig `yaml:"docs"`

	Host string `yaml:"host" env:"HTTP_HOST" env-default:"0.0.0.0"`
	Port string `yaml:"port" env:"HTTP_PORT" env-default:"8080"`

	MetricsPort string `yaml:"metrics_port" env:"HTTP_METRICS_PORT" env-default:"9090"`

	// TLS configuration для HTTP и HTTP/3 серверов
	TLS TLSConfig `yaml:"tls"`

	// Timeouts
	ReadTimeout       time.Duration `yaml:"read_timeout" env-default:"30s"`
	WriteTimeout      time.Duration `yaml:"write_timeout" env-default:"30s"`
	IdleTimeout       time.Duration `yaml:"idle_timeout" env-default:"120s"`
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout" env-default:"10s"`

	// Graceful shutdown timeout
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env-default:"30s"`

	// HTTP/3 support
	HTTP3Enabled bool `yaml:"http3_enabled" env:"HTTP_HTTP3_ENABLED" env-default:"false"`

	// Swagger documentation
	IsSwaggerEnabled bool `yaml:"swagger_enabled" env:"HTTP_SWAGGER_ENABLED" env-default:"false"`

	MetricsEnabled bool `yaml:"metrics_enabled" env:"HTTP_METRICS_ENABLED" env-default:"false"`

	// DisableBodyLogging выключает запись тел запросов/ответов (request_body, response_body)
	// в access-лог. По умолчанию тела логируются.
	DisableBodyLogging bool `yaml:"disable_body_logging" env:"HTTP_DISABLE_BODY_LOGGING" env-default:"false"`

	// Pprof profiling endpoints
	PprofEnabled bool `yaml:"pprof_enabled" env:"HTTP_PPROF_ENABLED" env-default:"false"`
}

func (c Config) Addr() string {
	return c.Host + ":" + c.Port
}
