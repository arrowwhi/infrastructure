package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// defaultServiceName используется как имя meter и tracer, если не задано иное.
const defaultServiceName = "postgres"

// Option настраивает коннектор программно. Включение и выключение инструментации
// доступно и через Config (переменные окружения) — опции нужны там, где требуется
// передать провайдеры, атрибуты или собственные хуки.
type Option func(*options)

type options struct {
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
	retryPolicy    func(error) bool
	beforeConnect  func(context.Context, *pgx.ConnConfig) error
	afterConnect   func(context.Context, *pgx.Conn) error
	forceTracing   *bool
	forceMetrics   *bool
	forceQueryLog  *bool
	forceSlowLog   *bool
	slowThreshold  *time.Duration
	serviceName    string
	attributes     []attribute.KeyValue
	extraTracers   []pgx.QueryTracer
}

// WithTracing включает трейсинг запросов независимо от значения в Config.
func WithTracing() Option {
	return func(o *options) {
		o.forceTracing = ptr(true)
	}
}

// WithoutTracing выключает трейсинг независимо от значения в Config.
func WithoutTracing() Option {
	return func(o *options) {
		o.forceTracing = ptr(false)
	}
}

// WithMetrics включает метрики запросов и пула независимо от значения в Config.
func WithMetrics() Option {
	return func(o *options) {
		o.forceMetrics = ptr(true)
	}
}

// WithoutMetrics выключает метрики независимо от значения в Config.
func WithoutMetrics() Option {
	return func(o *options) {
		o.forceMetrics = ptr(false)
	}
}

// WithQueryLogging включает лог каждого запроса. На нагруженном сервисе это дорого:
// включать осознанно и ненадолго.
func WithQueryLogging() Option {
	return func(o *options) {
		o.forceQueryLog = ptr(true)
	}
}

// WithSlowQueryLog включает лог запросов, превысивших порог.
func WithSlowQueryLog(threshold time.Duration) Option {
	return func(o *options) {
		o.forceSlowLog = ptr(true)
		o.slowThreshold = &threshold
	}
}

// WithTracerProvider задаёт источник трейсеров OpenTelemetry.
func WithTracerProvider(provider trace.TracerProvider) Option {
	return func(o *options) {
		o.tracerProvider = provider
	}
}

// WithMeterProvider задаёт источник метрик OpenTelemetry.
func WithMeterProvider(provider metric.MeterProvider) Option {
	return func(o *options) {
		o.meterProvider = provider
	}
}

// WithAttributes добавляет атрибуты ко всем span'ам и метрикам коннектора.
func WithAttributes(attrs ...attribute.KeyValue) Option {
	return func(o *options) {
		o.attributes = append(o.attributes, attrs...)
	}
}

// WithServiceName задаёт имя, под которым регистрируются meter и tracer.
func WithServiceName(name string) Option {
	return func(o *options) {
		o.serviceName = name
	}
}

// WithRetryPolicy расширяет политику повторов сверх безопасного минимума.
//
// По умолчанию запрос повторяется только когда pgx гарантирует, что он не дошёл
// до сервера. Своя политика снимает эту гарантию: повтор запроса, который мог
// выполниться, продублирует операцию. Для конфликтов сериализации и дедлоков
// повторять надо транзакцию целиком — это уже делает InTx.
func WithRetryPolicy(policy func(error) bool) Option {
	return func(o *options) {
		o.retryPolicy = policy
	}
}

// WithQueryTracer добавляет собственный трейсер pgx в общую цепочку.
func WithQueryTracer(tracer pgx.QueryTracer) Option {
	return func(o *options) {
		o.extraTracers = append(o.extraTracers, tracer)
	}
}

// WithBeforeConnect задаёт хук, вызываемый перед созданием каждого соединения.
//
// Основное применение — ротация секретов: пароль от Vault или IAM-токен живёт минуты,
// и хук позволяет подставить свежий, не перезапуская сервис.
func WithBeforeConnect(fn func(context.Context, *pgx.ConnConfig) error) Option {
	return func(o *options) {
		o.beforeConnect = fn
	}
}

// WithAfterConnect задаёт хук, вызываемый после установки соединения, — например,
// для регистрации собственных типов PostgreSQL.
func WithAfterConnect(fn func(context.Context, *pgx.Conn) error) Option {
	return func(o *options) {
		o.afterConnect = fn
	}
}

// newOptions собирает опции и накладывает их поверх конфигурации.
func newOptions(cfg *Config, opts []Option) options {
	resolved := options{serviceName: defaultServiceName}
	for _, opt := range opts {
		opt(&resolved)
	}

	if resolved.forceTracing != nil {
		cfg.TracingEnabled = *resolved.forceTracing
	}

	if resolved.forceMetrics != nil {
		cfg.MetricsEnabled = *resolved.forceMetrics
	}

	if resolved.forceQueryLog != nil {
		cfg.QueryLogEnabled = *resolved.forceQueryLog
	}

	if resolved.forceSlowLog != nil {
		cfg.SlowQueryLog = *resolved.forceSlowLog
	}

	if resolved.slowThreshold != nil {
		cfg.SlowQueryThreshold = *resolved.slowThreshold
	}

	if cfg.SlowQueryThreshold <= 0 {
		cfg.SlowQueryThreshold = defaultSlowQueryLimit
	}

	return resolved
}

func ptr[T any](v T) *T {
	return &v
}
