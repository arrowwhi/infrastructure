package telegram

import (
	"context"
	"errors"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/arrowwhi/infrastructure/observability/metrics"
)

type clientMetrics struct {
	requests    metric.Int64Counter
	duration    metric.Float64Histogram
	rateLimited metric.Int64Counter
	throttle    metric.Float64Histogram
}

func newClientMetrics(meter metric.Meter) (*clientMetrics, error) {
	requests, err := meter.Int64Counter("telegram.api.request.count",
		metric.WithDescription("Telegram Bot API calls by method and result"),
		metric.WithUnit("{request}"))
	if err != nil {
		return nil, err
	}

	duration, err := meter.Float64Histogram("telegram.api.request.duration",
		metric.WithDescription("Telegram Bot API call duration (getUpdates includes long polling)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(metrics.DurationBuckets...))
	if err != nil {
		return nil, err
	}

	rateLimited, err := meter.Int64Counter("telegram.api.rate_limited.count",
		metric.WithDescription("429 Too Many Requests responses from Telegram"),
		metric.WithUnit("{response}"))
	if err != nil {
		return nil, err
	}

	throttle, err := meter.Float64Histogram("telegram.api.throttle.duration",
		metric.WithDescription("Time spent waiting for the local rate limiter before a call"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(metrics.DurationBuckets...))
	if err != nil {
		return nil, err
	}

	return &clientMetrics{requests: requests, duration: duration, rateLimited: rateLimited, throttle: throttle}, nil
}

// recordCall пишет счётчик и длительность. status — "ok", код ошибки Bot API или "error".
func (m *clientMetrics) recordCall(ctx context.Context, method string, start time.Time, err error) {
	status := "ok"

	if err != nil {
		status = "error"

		var apiErr *APIError
		if errors.As(err, &apiErr) {
			status = strconv.Itoa(apiErr.Code)
		}
	}

	attrs := metric.WithAttributes(
		attribute.String("method", method),
		attribute.String("status", status),
	)

	m.requests.Add(ctx, 1, attrs)
	m.duration.Record(ctx, time.Since(start).Seconds(), attrs)
}

func (m *clientMetrics) recordThrottle(ctx context.Context, method string, waited time.Duration) {
	if waited < time.Millisecond {
		return
	}

	m.throttle.Record(ctx, waited.Seconds(), metric.WithAttributes(attribute.String("method", method)))
}
