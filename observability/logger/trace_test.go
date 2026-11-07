package logger

import (
	"bytes"
	"context"
	"testing"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

func spanContext(t *testing.T) (context.Context, trace.SpanContext) {
	t.Helper()

	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")

	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled})

	return trace.ContextWithSpanContext(context.Background(), sc), sc
}

func TestLog_AddsTraceIDs(t *testing.T) {
	var buf bytes.Buffer

	resetGlobal()
	defer resetGlobal()

	global = &Logger{logger: zerolog.New(&buf)}

	ctx, sc := spanContext(t)
	Error(ctx).Msg("boom")

	m := readJSON(t, &buf)
	if m[TraceIDKey] != sc.TraceID().String() {
		t.Errorf("trace_id = %v, want %s", m[TraceIDKey], sc.TraceID())
	}

	if m[SpanIDKey] != sc.SpanID().String() {
		t.Errorf("span_id = %v, want %s", m[SpanIDKey], sc.SpanID())
	}
}

func TestLog_NoSpanNoTraceIDs(t *testing.T) {
	var buf bytes.Buffer

	resetGlobal()
	defer resetGlobal()

	global = &Logger{logger: zerolog.New(&buf)}

	Info(context.Background()).Msg("plain")

	m := readJSON(t, &buf)
	if _, ok := m[TraceIDKey]; ok {
		t.Errorf("trace_id must be absent without a span, got %v", m[TraceIDKey])
	}
}

func TestTraceHook(t *testing.T) {
	var buf bytes.Buffer

	zl := zerolog.New(&buf).Hook(TraceHook{})
	ctx, sc := spanContext(t)

	zl.Info().Ctx(ctx).Msg("hooked")

	m := readJSON(t, &buf)
	if m[TraceIDKey] != sc.TraceID().String() {
		t.Errorf("trace_id = %v, want %s", m[TraceIDKey], sc.TraceID())
	}
}
