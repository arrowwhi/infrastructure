// Package redis provides an instrumented Redis client built on go-redis v9.
//
// Клиент добавляет к go-redis: проверку соединения при старте, трейсы и метрики
// OpenTelemetry (redisotel: спан на команду, длительность команд, статистика пула)
// и Healthcheck для observability/health. Сам *redis.Client доступен встраиванием,
// поэтому весь API go-redis работает как обычно, а клиент можно передать в txmanager.
package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	goredis "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Config — настройки подключения к Redis.
type Config struct {
	Addr     string `yaml:"addr" env:"REDIS_ADDR" env-default:"localhost:6379"`
	Username string `yaml:"username" env:"REDIS_USERNAME"`
	Password string `yaml:"password" env:"REDIS_PASSWORD"`
	DB       int    `yaml:"db" env:"REDIS_DB" env-default:"0"`

	PoolSize     int `yaml:"pool_size" env:"REDIS_POOL_SIZE" env-default:"0"` // 0 — 10 × GOMAXPROCS
	MinIdleConns int `yaml:"min_idle_conns" env:"REDIS_MIN_IDLE_CONNS" env-default:"0"`

	DialTimeout  time.Duration `yaml:"dial_timeout" env:"REDIS_DIAL_TIMEOUT" env-default:"5s"`
	ReadTimeout  time.Duration `yaml:"read_timeout" env:"REDIS_READ_TIMEOUT" env-default:"3s"`
	WriteTimeout time.Duration `yaml:"write_timeout" env:"REDIS_WRITE_TIMEOUT" env-default:"3s"`
	PoolTimeout  time.Duration `yaml:"pool_timeout" env:"REDIS_POOL_TIMEOUT" env-default:"4s"`

	TLSEnabled bool `yaml:"tls_enabled" env:"REDIS_TLS_ENABLED" env-default:"false"`

	// TracingEnabled — спан на каждую команду/конвейер. TracerProvider должен быть инициализирован.
	TracingEnabled bool `yaml:"tracing_enabled" env:"REDIS_TRACING_ENABLED" env-default:"true"`
	// MetricsEnabled — метрики db.client.* (длительность команд, соединения пула).
	MetricsEnabled bool `yaml:"metrics_enabled" env:"REDIS_METRICS_ENABLED" env-default:"true"`
}

// Client — клиент Redis с инструментацией.
type Client struct {
	*goredis.Client
}

type options struct {
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
}

// Option настраивает клиент.
type Option func(*options)

// WithTracerProvider задаёт TracerProvider (по умолчанию глобальный).
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(o *options) { o.tracerProvider = tp }
}

// WithMeterProvider задаёт MeterProvider (по умолчанию глобальный).
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(o *options) { o.meterProvider = mp }
}

// New создаёт клиента, подключает инструментацию и проверяет соединение (PING).
func New(ctx context.Context, cfg Config, opts ...Option) (*Client, error) {
	if cfg.Addr == "" {
		return nil, errors.New("redis: addr is required")
	}

	var o options
	for _, opt := range opts {
		opt(&o)
	}

	redisOpts := &goredis.Options{
		Addr:         cfg.Addr,
		Username:     cfg.Username,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		PoolTimeout:  cfg.PoolTimeout,
	}

	if cfg.TLSEnabled {
		redisOpts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	client := goredis.NewClient(redisOpts)

	if err := instrument(client, cfg, o); err != nil {
		_ = client.Close()

		return nil, err
	}

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()

		return nil, fmt.Errorf("redis: ping %s: %w", cfg.Addr, err)
	}

	return &Client{Client: client}, nil
}

func instrument(client *goredis.Client, cfg Config, o options) error {
	if cfg.TracingEnabled {
		var traceOpts []redisotel.TracingOption
		if o.tracerProvider != nil {
			traceOpts = append(traceOpts, redisotel.WithTracerProvider(o.tracerProvider))
		}

		if err := redisotel.InstrumentTracing(client, traceOpts...); err != nil {
			return fmt.Errorf("redis: instrument tracing: %w", err)
		}
	}

	if cfg.MetricsEnabled {
		var metricOpts []redisotel.MetricsOption
		if o.meterProvider != nil {
			metricOpts = append(metricOpts, redisotel.WithMeterProvider(o.meterProvider))
		}

		if err := redisotel.InstrumentMetrics(client, metricOpts...); err != nil {
			return fmt.Errorf("redis: instrument metrics: %w", err)
		}
	}

	return nil
}

// Healthcheck — проверка для observability/health. PING идёт через пул, поэтому при
// исчерпанном пуле проверка упадёт по таймауту ожидания соединения.
func (c *Client) Healthcheck(ctx context.Context) error {
	if err := c.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping: %w", err)
	}

	return nil
}
