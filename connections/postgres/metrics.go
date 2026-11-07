package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	metricQueryDuration = "db.client.operation.duration"
	metricQueryErrors   = "db.client.operation.errors"
	metricBatchSize     = "db.client.batch.size"

	attrQueryName  = "db.query.name"
	attrOperation  = "db.operation.name"
	attrStatusCode = "db.response.status_code"
	attrOutcome    = "db.operation.outcome"

	outcomeOK    = "ok"
	outcomeError = "error"
)

// metricsCtxKey хранит состояние измерения между началом и концом запроса.
type metricsCtxKey struct{}

// metricsState — то, что известно на старте запроса. Операцию определяем именно здесь:
// в конце CommandTag пуст, если запрос завершился ошибкой, а операция нужна как раз
// для разбора ошибок.
type metricsState struct {
	start     time.Time
	operation string
}

// metricsTracer снимает длительность и ошибки запросов.
//
// Собственный трейсер, а не метрики otelpgx, нужен ради метки db.query.name: без неё
// по метрикам нельзя найти конкретный медленный запрос, а разбивка по тексту SQL
// взорвала бы кардинальность.
type metricsTracer struct {
	duration  metric.Float64Histogram
	errors    metric.Int64Counter
	batchSize metric.Int64Histogram
	warner    *unnamedWarner
	attrs     []attribute.KeyValue
}

var (
	_ pgx.QueryTracer = (*metricsTracer)(nil)
	_ pgx.BatchTracer = (*metricsTracer)(nil)
)

// newMetricsTracer создаёт инструменты метрик на переданном meter.
func newMetricsTracer(meter metric.Meter, attrs []attribute.KeyValue) (*metricsTracer, error) {
	duration, err := meter.Float64Histogram(
		metricQueryDuration,
		metric.WithDescription("Duration of database queries"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create %s histogram: %w", metricQueryDuration, err)
	}

	errCounter, err := meter.Int64Counter(
		metricQueryErrors,
		metric.WithDescription("Number of failed database queries by SQLSTATE"),
	)
	if err != nil {
		return nil, fmt.Errorf("create %s counter: %w", metricQueryErrors, err)
	}

	batchSize, err := meter.Int64Histogram(
		metricBatchSize,
		metric.WithDescription("Number of queries sent in a single batch"),
	)
	if err != nil {
		return nil, fmt.Errorf("create %s histogram: %w", metricBatchSize, err)
	}

	return &metricsTracer{
		duration:  duration,
		errors:    errCounter,
		batchSize: batchSize,
		warner:    &unnamedWarner{},
		attrs:     attrs,
	}, nil
}

func (m *metricsTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if QueryNameFromContext(ctx) == UnknownQueryName && !isTxControl(data.SQL) {
		m.warner.warn(ctx, data.SQL)
	}

	return context.WithValue(ctx, metricsCtxKey{}, metricsState{start: time.Now(), operation: operationOf(data.SQL)})
}

func (m *metricsTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	m.record(ctx, data.Err)
}

func (m *metricsTracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	m.batchSize.Record(ctx, int64(data.Batch.Len()), metric.WithAttributes(m.baseAttrs(ctx)...))

	return context.WithValue(ctx, metricsCtxKey{}, metricsState{start: time.Now(), operation: "BATCH"})
}

func (m *metricsTracer) TraceBatchQuery(_ context.Context, _ *pgx.Conn, _ pgx.TraceBatchQueryData) {
	// Отдельные запросы пачки не измеряем: значение имеет длительность всей пачки,
	// именно она соответствует одному сетевому обмену.
}

func (m *metricsTracer) TraceBatchEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchEndData) {
	m.record(ctx, data.Err)
}

// record фиксирует длительность и, при наличии, ошибку.
func (m *metricsTracer) record(ctx context.Context, err error) {
	state, ok := ctx.Value(metricsCtxKey{}).(metricsState)
	if !ok {
		return
	}

	attrs := append(m.baseAttrs(ctx), attribute.String(attrOperation, state.operation))

	if err != nil {
		state := SQLState(err)
		if state == "" {
			state = UnknownQueryName
		}

		attrs = append(attrs, attribute.String(attrOutcome, outcomeError), attribute.String(attrStatusCode, state))
		m.errors.Add(ctx, 1, metric.WithAttributes(attrs...))
	} else {
		attrs = append(attrs, attribute.String(attrOutcome, outcomeOK))
	}

	m.duration.Record(ctx, time.Since(state.start).Seconds(), metric.WithAttributes(attrs...))
}

// baseAttrs возвращает общие атрибуты запроса.
func (m *metricsTracer) baseAttrs(ctx context.Context) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(m.attrs)+3)
	attrs = append(attrs, m.attrs...)

	return append(attrs, attribute.String(attrQueryName, QueryNameFromContext(ctx)))
}
