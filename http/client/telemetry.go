package httpclient

import (
	"context"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

const defaultServiceName = "http-client"

func (c Config) serviceName() string {
	if c.ServiceName != "" {
		return c.ServiceName
	}

	return defaultServiceName
}

// instrumentTransport оборачивает транспорт в otelhttp. Каждая попытка (включая ретраи)
// получает свой спан и попадает в метрики http.client.request.duration и
// http.client.request.body.size / response.body.size по семантическим соглашениям OTel.
func instrumentTransport(cfg Config, base http.RoundTripper) http.RoundTripper {
	if !cfg.TracingEnabled && !cfg.MetricsEnabled {
		return base
	}

	opts := []otelhttp.Option{
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return "HTTP " + r.Method + " " + r.URL.Host
		}),
	}

	if !cfg.TracingEnabled {
		opts = append(opts, otelhttp.WithTracerProvider(tracenoop.NewTracerProvider()))
	}

	if !cfg.MetricsEnabled {
		opts = append(opts, otelhttp.WithMeterProvider(metricnoop.NewMeterProvider()))
	}

	return otelhttp.NewTransport(base, opts...)
}

// newRetryCounter создаёт счётчик повторов или nil, если метрики выключены.
func newRetryCounter(cfg Config) (metric.Int64Counter, error) {
	if !cfg.MetricsEnabled {
		return nil, nil //nolint:nilnil // выключенные метрики — валидное состояние
	}

	return otel.Meter(cfg.serviceName()).Int64Counter(
		"http.client.retry.count",
		metric.WithDescription("Number of HTTP client retry attempts"),
		metric.WithUnit("{retry}"),
	)
}

func (c *client) recordRetry(ctx context.Context, method string) {
	if c.retryCounter == nil {
		return
	}

	c.retryCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("http.request.method", method)))
}
