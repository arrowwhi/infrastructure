package httpclient

import "time"

type Config struct {
	BaseURL string `yaml:"base_url" env:"HTTP_CLIENT_BASE_URL" required:"true"`

	DialTimeout           time.Duration `yaml:"dial_timeout" env:"HTTP_CLIENT_DIAL_TIMEOUT" env-default:"30s"`
	TLSHandshakeTimeout   time.Duration `yaml:"tls_handshake_timeout" env:"HTTP_CLIENT_TLS_HANDSHAKE_TIMEOUT" env-default:"10s"`
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout" env:"HTTP_CLIENT_RESPONSE_HEADER_TIMEOUT" env-default:"30s"`

	MaxIdleConns    int           `yaml:"max_idle_conns" env:"HTTP_CLIENT_MAX_IDLE_CONNS" env-default:"10"`
	IdleConnTimeout time.Duration `yaml:"idle_conn_timeout" env:"HTTP_CLIENT_IDLE_CONN_TIMEOUT" env-default:"90s"`

	TLS *TLSConfig `yaml:"tls" env-default:"{}"`

	Retry *RetryConfig `yaml:"retry" env-default:"{}"`

	// ServiceName — имя для meter'а и tracer'а; если пусто, используется "http-client".
	ServiceName string `yaml:"service_name" env:"HTTP_CLIENT_SERVICE_NAME"`

	// TracingEnabled — спан на каждую попытку запроса и проброс trace context (traceparent)
	// в заголовки. TracerProvider должен быть инициализирован.
	TracingEnabled bool `yaml:"tracing_enabled" env:"HTTP_CLIENT_TRACING_ENABLED" env-default:"true"`
	// MetricsEnabled — метрики http.client.* (длительность, размеры, число ретраев).
	MetricsEnabled bool `yaml:"metrics_enabled" env:"HTTP_CLIENT_METRICS_ENABLED" env-default:"true"`
}

type TLSConfig struct {
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify" env:"HTTP_CLIENT_TLS_INSECURE_SKIP_VERIFY" env-default:"false"`
	RootCAs            []byte `yaml:"root_cas" env:"HTTP_CLIENT_TLS_ROOT_CAS"`
	ClientCert         []byte `yaml:"client_cert" env:"HTTP_CLIENT_TLS_CLIENT_CERT"`
	ClientKey          []byte `yaml:"client_key" env:"HTTP_CLIENT_TLS_CLIENT_KEY"`
}

type RetryConfig struct {
	MaxRetries      uint          `yaml:"max_retries" env:"HTTP_CLIENT_RETRY_MAX_RETRIES" env-default:"3"`
	BackoffTime     time.Duration `yaml:"backoff_time" env:"HTTP_CLIENT_RETRY_BACKOFF_TIME" env-default:"100ms"`
	BackoffFraction float64       `yaml:"backoff_fraction" env:"HTTP_CLIENT_RETRY_BACKOFF_FRACTION" env-default:"0.3"`
}
