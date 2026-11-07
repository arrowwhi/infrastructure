// Package postgres provides an instrumented PostgreSQL connection pool built on pgx.
//
// Пул оборачивает pgxpool и добавляет: трейсинг и метрики OpenTelemetry, логирование
// медленных запросов, безопасные повторы, транзакции через контекст, разделение
// чтения и записи, а также набор хелперов для массовых операций.
//
// Инструментация выключена по умолчанию и включается через Config (переменные
// окружения) либо опциями WithTracing/WithMetrics. Когда всё выключено, трейсер
// не создаётся вовсе и накладных расходов нет.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	metric_noop "go.opentelemetry.io/otel/metric/noop"
)

// redactedValue подставляется вместо пароля в текстах ошибок.
const redactedValue = "***"

// Pool — пул соединений с PostgreSQL.
type Pool struct {
	primary     *pgxpool.Pool
	replicas    *pgxpool.Pool
	retryPolicy func(error) bool
	cfg         Config
	closeOnce   sync.Once
}

// New создаёт пул соединений и проверяет доступность базы.
func New(ctx context.Context, cfg Config, opts ...Option) (*Pool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	resolved := newOptions(&cfg, opts)

	tracer, err := buildTracer(cfg, resolved)
	if err != nil {
		return nil, err
	}

	primary, err := newPgxPool(ctx, cfg, cfg.connString(), tracer, resolved)
	if err != nil {
		return nil, fmt.Errorf("primary: %w", err)
	}

	pool := &Pool{primary: primary, cfg: cfg, retryPolicy: resolved.retryPolicy}

	if strings.TrimSpace(cfg.ReplicaHosts) != "" {
		replicas, replicaErr := newPgxPool(ctx, cfg, cfg.replicaConnString(), tracer, resolved)
		if replicaErr != nil {
			primary.Close()

			return nil, fmt.Errorf("replicas: %w", replicaErr)
		}

		pool.replicas = replicas
	}

	if cfg.MetricsEnabled {
		if statsErr := pool.recordPoolStats(resolved); statsErr != nil {
			pool.Close()

			return nil, statsErr
		}
	}

	return pool, nil
}

// Close закрывает пулы. Повторный вызов безопасен.
func (p *Pool) Close() {
	p.closeOnce.Do(func() {
		if p.replicas != nil {
			p.replicas.Close()
		}

		if p.primary != nil {
			p.primary.Close()
		}
	})
}

// Raw возвращает нижележащий пул pgx для операций, не покрытых коннектором.
func (p *Pool) Raw() *pgxpool.Pool {
	return p.primary
}

// Stat возвращает статистику пула мастера.
func (p *Pool) Stat() *pgxpool.Stat {
	if p.primary == nil {
		return nil
	}

	return p.primary.Stat()
}

// Ping проверяет доступность базы.
func (p *Pool) Ping(ctx context.Context) error {
	if p.primary == nil {
		return ErrClosed
	}

	if err := p.primary.Ping(ctx); err != nil {
		return fmt.Errorf("ping primary: %w", err)
	}

	return nil
}

// Healthcheck пригоден для readiness-пробы: проверяет не только доступность базы,
// но и то, что пул не исчерпан, — иначе под считается живым ровно в тот момент,
// когда все соединения заняты и запросы стоят в очереди.
func (p *Pool) Healthcheck(ctx context.Context) error {
	if p.primary == nil {
		return ErrClosed
	}

	start := time.Now()

	if err := p.primary.Ping(ctx); err != nil {
		return fmt.Errorf("ping primary: %w", err)
	}

	if acquire := time.Since(start); p.cfg.HealthcheckAcquireThreshold > 0 && acquire > p.cfg.HealthcheckAcquireThreshold {
		return fmt.Errorf("%w: acquire took %s", ErrPoolExhausted, acquire)
	}

	stat := p.primary.Stat()
	if stat.MaxConns() > 0 && stat.AcquiredConns() >= stat.MaxConns() {
		return fmt.Errorf("%w: %d/%d connections acquired", ErrPoolExhausted, stat.AcquiredConns(), stat.MaxConns())
	}

	if p.replicas != nil {
		if err := p.replicas.Ping(ctx); err != nil {
			return fmt.Errorf("ping replicas: %w", err)
		}
	}

	return nil
}

// ReplicationLag возвращает отставание реплики от мастера.
// Вызывается на пуле реплик; на мастере значение лишено смысла.
func (p *Pool) ReplicationLag(ctx context.Context) (time.Duration, error) {
	if p.replicas == nil {
		return 0, ErrNoReplicas
	}

	var seconds *float64

	const query = `SELECT EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp()))`

	if err := p.replicas.QueryRow(ctx, query).Scan(&seconds); err != nil {
		return 0, fmt.Errorf("query replication lag: %w", err)
	}

	// NULL означает, что узел не является репликой и ничего не проигрывает.
	if seconds == nil {
		return 0, nil
	}

	return time.Duration(*seconds * float64(time.Second)), nil
}

// recordPoolStats подключает сбор метрик пула (занятые и свободные соединения,
// время ожидания, число неудачных попыток получить соединение).
func (p *Pool) recordPoolStats(o options) error {
	statsOpts := []otelpgx.StatsOption{
		otelpgx.WithStatsAttributes(o.attributes...),
	}

	if o.meterProvider != nil {
		statsOpts = append(statsOpts, otelpgx.WithStatsMeterProvider(o.meterProvider))
	}

	if err := otelpgx.RecordStats(p.primary, statsOpts...); err != nil {
		return fmt.Errorf("record pool stats: %w", err)
	}

	if p.replicas != nil {
		if err := otelpgx.RecordStats(p.replicas, statsOpts...); err != nil {
			return fmt.Errorf("record replica pool stats: %w", err)
		}
	}

	return nil
}

// buildTracer собирает цепочку трейсеров. Возвращает nil, когда инструментация выключена
// полностью: тогда pgx не вызывает трейсер вообще.
func buildTracer(cfg Config, o options) (pgx.QueryTracer, error) {
	if !cfg.instrumented() {
		return nil, nil //nolint:nilnil // выключенная инструментация — валидное состояние
	}

	tracers := make([]pgx.QueryTracer, 0, 4)

	if cfg.TracingEnabled {
		otelOpts := []otelpgx.Option{
			// Метрики снимает собственный трейсер: ему нужна метка db.query.name,
			// которой otelpgx не знает. Пустой провайдер исключает двойной учёт.
			otelpgx.WithMeterProvider(metric_noop.NewMeterProvider()),
			otelpgx.WithTracerAttributes(o.attributes...),
		}

		if o.tracerProvider != nil {
			otelOpts = append(otelOpts, otelpgx.WithTracerProvider(o.tracerProvider))
		}

		tracers = append(tracers, otelpgx.NewTracer(otelOpts...))
	}

	if cfg.MetricsEnabled {
		provider := o.meterProvider
		if provider == nil {
			provider = otel.GetMeterProvider()
		}

		metricsTracer, err := newMetricsTracer(provider.Meter(o.serviceName), o.attributes)
		if err != nil {
			return nil, err
		}

		tracers = append(tracers, metricsTracer)
	}

	if logTracer := newLogTracer(cfg.QueryLogEnabled, cfg.SlowQueryLog, cfg.SlowQueryThreshold); logTracer != nil {
		tracers = append(tracers, logTracer)
	}

	tracers = append(tracers, o.extraTracers...)

	return newCompositeTracer(tracers...), nil
}

// newPgxPool собирает и открывает один пул pgx.
func newPgxPool(ctx context.Context, cfg Config, connString string, tracer pgx.QueryTracer, o options) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", redact(err, cfg.Password))
	}

	applyPoolSettings(poolCfg, cfg)

	if err = applyConnSettings(poolCfg, cfg, tracer); err != nil {
		return nil, err
	}

	if o.beforeConnect != nil {
		poolCfg.BeforeConnect = o.beforeConnect
	}

	if o.afterConnect != nil {
		poolCfg.AfterConnect = o.afterConnect
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", redact(err, cfg.Password))
	}

	if err = pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("ping: %w", redact(err, cfg.Password))
	}

	return pool, nil
}

// applyPoolSettings переносит настройки размера и времени жизни соединений.
func applyPoolSettings(poolCfg *pgxpool.Config, cfg Config) {
	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}

	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}

	if cfg.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}

	if cfg.MaxConnIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
}

// applyConnSettings настраивает соединение: трейсер, режим выполнения запросов,
// серверные таймауты и TLS.
func applyConnSettings(poolCfg *pgxpool.Config, cfg Config, tracer pgx.QueryTracer) error {
	poolCfg.ConnConfig.Tracer = tracer

	mode, err := parseQueryExecMode(cfg.QueryExecMode)
	if err != nil {
		return err
	}

	poolCfg.ConnConfig.DefaultQueryExecMode = mode

	if poolCfg.ConnConfig.RuntimeParams == nil {
		poolCfg.ConnConfig.RuntimeParams = make(map[string]string)
	}

	maps.Copy(poolCfg.ConnConfig.RuntimeParams, cfg.runtimeParams())

	tlsCfg, err := cfg.TLS.tlsConfig()
	if err != nil {
		return err
	}

	if tlsCfg != nil {
		poolCfg.ConnConfig.TLSConfig = tlsCfg
	}

	return nil
}

// redact убирает пароль из текста ошибки: pgx охотно вкладывает в неё строку
// подключения целиком, а она попадает в логи.
func redact(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}

	message := err.Error()
	if !strings.Contains(message, secret) {
		return err
	}

	return errors.New(strings.ReplaceAll(message, secret, redactedValue))
}
