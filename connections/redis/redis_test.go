package redis

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestClient_InstrumentedRoundTrip(t *testing.T) {
	srv := miniredis.RunT(t)

	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	ctx := context.Background()

	cfg := Config{Addr: srv.Addr(), TracingEnabled: true, MetricsEnabled: true}

	client, err := New(ctx, cfg, WithTracerProvider(tp), WithMeterProvider(mp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.Set(ctx, "k", "v", 0).Err())
	assert.Equal(t, "v", client.Get(ctx, "k").Val())
	require.NoError(t, client.Healthcheck(ctx))

	assert.NotEmpty(t, spans.Ended(), "команды должны порождать спаны")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	require.NotEmpty(t, rm.ScopeMetrics, "должны быть метрики redisotel")
}

func TestNew_FailsWhenUnavailable(t *testing.T) {
	srv := miniredis.RunT(t)
	addr := srv.Addr()
	srv.Close()

	_, err := New(context.Background(), Config{Addr: addr})
	require.Error(t, err)
}

func TestNew_RequiresAddr(t *testing.T) {
	_, err := New(context.Background(), Config{})
	require.Error(t, err)
}
