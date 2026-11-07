package tracing

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

func newTestProvider(t *testing.T, exporter string, rate float64) *TracerProvider {
	t.Helper()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exporter = exporter
	cfg.OTLPEndpoint = "127.0.0.1:1" // экспорт никуда не уходит, проверяем только сэмплинг
	cfg.SamplingRate = rate

	tp, err := InitTracer(context.Background(), cfg, "test", "v0", "test")
	if err != nil {
		t.Fatalf("InitTracer: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_ = tp.Shutdown(ctx)
	})

	return tp
}

func TestSampler_RespectsSampledParent(t *testing.T) {
	tp := newTestProvider(t, "otlp-http", 0) // корневые спаны не сэмплируются

	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})

	ctx := trace.ContextWithRemoteSpanContext(context.Background(), parent)

	_, child := tp.Tracer("test").Start(ctx, "child")
	defer child.End()

	if !child.SpanContext().IsSampled() {
		t.Fatal("child of a sampled remote parent must be sampled (ParentBased)")
	}

	_, root := tp.Tracer("test").Start(context.Background(), "root")
	defer root.End()

	if root.SpanContext().IsSampled() {
		t.Fatal("root span must follow SamplingRate=0")
	}
}

func TestInitTracer_Exporters(t *testing.T) {
	for _, exp := range []string{"otlp-grpc", "otlp-http"} {
		t.Run(exp, func(t *testing.T) {
			newTestProvider(t, exp, 1)
		})
	}

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exporter = "nope"

	if _, err := InitTracer(context.Background(), cfg, "test", "v0", "test"); err == nil {
		t.Fatal("unknown exporter must fail")
	}
}

func TestInitTracer_DisabledByDefault(t *testing.T) {
	tp, err := InitTracer(context.Background(), DefaultConfig(), "test", "v0", "test")
	if err != nil {
		t.Fatalf("InitTracer: %v", err)
	}

	_, span := tp.Tracer("test").Start(context.Background(), "noop")
	defer span.End()

	if span.SpanContext().IsValid() {
		t.Fatal("tracing is disabled by default, span must be no-op")
	}
}
