package metrics

const (
	// defaultMetricIntervalSeconds is the default interval for exporting metrics in seconds
	defaultMetricIntervalSeconds = 60
)

// Config определяет конфигурацию для сбора метрик
type Config struct {
	// Exporter - тип экспортера ("prometheus", "otlp-grpc", "otlp-http")
	Exporter           string `yaml:"exporter" env:"EXPORTER" env-default:"prometheus"`
	PrometheusEndpoint string `yaml:"prometheus_endpoint" env:"PROMETHEUS_ENDPOINT" env-default:":9090"`
	// OTLPEndpoint - адрес OTLP коллектора (например, "localhost:4317")
	OTLPEndpoint string `yaml:"otlp_endpoint" env:"OTLP_ENDPOINT" env-default:"localhost:4317"`
	// MetricInterval - интервал экспорта метрик
	MetricInterval int64 `yaml:"metric_interval" env:"METRIC_INTERVAL" env-default:"60"` // в секундах
	Enabled        bool  `yaml:"enabled" env:"ENABLED" env-default:"true"`
	// OTLPInsecure - использовать ли небезопасное соединение для OTLP
	OTLPInsecure bool `yaml:"otlp_insecure" env:"OTLP_INSECURE" env-default:"true"`
	// RuntimeMetrics - метрики рантайма Go (память, GC, горутины) для OTLP-экспортеров.
	// Для Prometheus они уже есть в стандартном реестре (go_*, process_*).
	RuntimeMetrics bool `yaml:"runtime_metrics" env:"RUNTIME_METRICS" env-default:"true"`
}

// DefaultConfig возвращает конфигурацию по умолчанию
func DefaultConfig() Config {
	return Config{
		Enabled:            true,
		Exporter:           "prometheus",
		PrometheusEndpoint: ":9090",
		OTLPEndpoint:       "localhost:4317",
		OTLPInsecure:       true,
		MetricInterval:     defaultMetricIntervalSeconds,
		RuntimeMetrics:     true,
	}
}
