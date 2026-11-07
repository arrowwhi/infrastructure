package health

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// RegisterMetrics публикует результаты последнего Check:
//   - health.check.status — 1 (ok) или 0 (fail) с метками check и critical;
//   - health.check.duration — длительность проверки в секундах.
//
// Метрики отражают Registry.Last, поэтому проверки должны выполняться регулярно —
// пробами Kubernetes, Registry.Watch или gRPC-сервером с WithHealthRegistry.
func (r *Registry) RegisterMetrics(meter metric.Meter) error {
	status, err := meter.Int64ObservableGauge(
		"health.check.status",
		metric.WithDescription("Result of the last health check: 1 = ok, 0 = fail"),
	)
	if err != nil {
		return fmt.Errorf("health status gauge: %w", err)
	}

	duration, err := meter.Float64ObservableGauge(
		"health.check.duration",
		metric.WithDescription("Duration of the last health check"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("health duration gauge: %w", err)
	}

	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		for name, res := range r.Last().Checks {
			attrs := metric.WithAttributes(
				attribute.String("check", name),
				attribute.Bool("critical", res.Critical),
			)

			var value int64
			if res.Status == StatusOK {
				value = 1
			}

			o.ObserveInt64(status, value, attrs)
			o.ObserveFloat64(duration, res.Duration.Seconds(), attrs)
		}

		return nil
	}, status, duration)
	if err != nil {
		return fmt.Errorf("health metrics callback: %w", err)
	}

	return nil
}
