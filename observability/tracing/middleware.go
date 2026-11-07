package tracing

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

func Middleware(serviceName string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		handler := otelhttp.NewHandler(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				span := trace.SpanFromContext(r.Context())

				rctx := chi.RouteContext(r.Context())
				if rctx != nil && rctx.RoutePattern() != "" {
					span.SetAttributes(
						semconv.HTTPRouteKey.String(rctx.RoutePattern()),
					)
				}

				if r.URL.RawQuery != "" {
					span.SetAttributes(
						attribute.String("http.query", r.URL.RawQuery),
					)
				}

				next.ServeHTTP(w, r)
			}),
			serviceName,
			otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
				rctx := chi.RouteContext(r.Context())
				if rctx != nil && rctx.RoutePattern() != "" {
					return r.Method + " " + rctx.RoutePattern()
				}

				return r.Method + " " + r.URL.Path
			}),
		)

		return handler
	}
}
