package bot

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/arrowwhi/infrastructure/observability/metrics"
)

type botMetrics struct {
	meter    metric.Meter
	updates  metric.Int64Counter
	duration metric.Float64Histogram
}

func newBotMetrics(meter metric.Meter) (*botMetrics, error) {
	updates, err := meter.Int64Counter("telegram.bot.updates.count",
		metric.WithDescription("Telegram updates by type, route and status (ok, error, panic, unhandled)"),
		metric.WithUnit("{update}"))
	if err != nil {
		return nil, err
	}

	duration, err := meter.Float64Histogram("telegram.bot.update.duration",
		metric.WithDescription("Time to handle a Telegram update"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(metrics.DurationBuckets...))
	if err != nil {
		return nil, err
	}

	return &botMetrics{meter: meter, updates: updates, duration: duration}, nil
}

func (m *botMetrics) recordUpdate(ctx context.Context, updateType, route, status string, d time.Duration) {
	m.updates.Add(ctx, 1, metric.WithAttributes(
		attribute.String("type", updateType),
		attribute.String("route", route),
		attribute.String("status", status),
	))

	m.duration.Record(ctx, d.Seconds(), metric.WithAttributes(
		attribute.String("type", updateType),
		attribute.String("route", route),
	))
}

// observeQueue публикует telegram.bot.queue.size — сколько обновлений ждёт обработки.
// Растущая очередь означает, что обработчики не успевают.
func (m *botMetrics) observeQueue(d *dispatcher) error {
	_, err := m.meter.Int64ObservableGauge("telegram.bot.queue.size",
		metric.WithDescription("Telegram updates waiting in worker queues"),
		metric.WithUnit("{update}"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(int64(d.size()))

			return nil
		}),
	)

	return err
}
