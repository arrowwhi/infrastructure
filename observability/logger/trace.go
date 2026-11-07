package logger

import (
	"context"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

const (
	// TraceIDKey и SpanIDKey — имена полей с идентификаторами трейса в логах.
	// Совпадают с соглашением OpenTelemetry для логов, поэтому Grafana/Loki и Jaeger
	// связывают записи и спаны без дополнительной настройки.
	TraceIDKey = "trace_id"
	SpanIDKey  = "span_id"
)

// withTrace добавляет trace_id и span_id, если в контексте есть валидный спан.
func withTrace(evt *zerolog.Event, ctx context.Context) *zerolog.Event {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return evt
	}

	return evt.
		Str(TraceIDKey, sc.TraceID().String()).
		Str(SpanIDKey, sc.SpanID().String())
}

// TraceHook — zerolog.Hook, добавляющий trace_id/span_id в логгеры, созданные напрямую
// через zerolog (например, переданные в grpc/client). Событие должно получить контекст
// через Event.Ctx(ctx): logger.Hook(TraceHook{}).Info().Ctx(ctx).Msg("...").
type TraceHook struct{}

// Run реализует zerolog.Hook.
func (TraceHook) Run(e *zerolog.Event, _ zerolog.Level, _ string) {
	ctx := e.GetCtx()
	if ctx == nil {
		return
	}

	withTrace(e, ctx)
}
