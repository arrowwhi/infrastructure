package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// unmatchedRoute — значение метки http.route, когда шаблон маршрута неизвестен
// (запрос не попал ни в один маршрут chi или роутер не chi). Подставлять сырой
// путь нельзя: /users/1, /users/2, … дают по временному ряду на каждый id.
const unmatchedRoute = "unmatched"

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    int64
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.written += int64(n)

	return n, err
}

// Unwrap нужен http.ResponseController (Flush, Hijack и т.п. через обёртку).
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Middleware возвращает middleware для сбора HTTP метрик.
//
// Метка http.route — шаблон маршрута chi (например, /users/{id}), а не сырой путь.
// Шаблон известен только после маршрутизации, поэтому метрики пишутся после вызова
// обработчика. Middleware нужно вешать на роутер chi (router.Use), а не оборачивать
// им роутер снаружи — иначе шаблон будет недоступен и метка станет "unmatched".
func Middleware(serviceName string) func(next http.Handler) http.Handler {
	meter := otel.Meter(serviceName)

	requestCounter, _ := meter.Int64Counter(
		"http.server.request.count",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("{request}"),
	)

	requestDuration, _ := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithDescription("HTTP request duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(DurationBuckets...),
	)

	requestSize, _ := meter.Int64Histogram(
		"http.server.request.size",
		metric.WithDescription("HTTP request size"),
		metric.WithUnit("By"),
	)

	responseSize, _ := meter.Int64Histogram(
		"http.server.response.size",
		metric.WithDescription("HTTP response size"),
		metric.WithUnit("By"),
	)

	activeRequests, _ := meter.Int64UpDownCounter(
		"http.server.active_requests",
		metric.WithDescription("Number of active HTTP requests"),
		metric.WithUnit("{request}"),
	)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx := r.Context()

			rw := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Маршрут до обработки ещё неизвестен, поэтому активные запросы — только по методу.
			activeAttrs := metric.WithAttributes(attribute.String("http.method", r.Method))
			activeRequests.Add(ctx, 1, activeAttrs)

			defer activeRequests.Add(ctx, -1, activeAttrs)

			next.ServeHTTP(rw, r)

			attrs := metric.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.route", routePattern(r)),
				attribute.String("http.status_code", strconv.Itoa(rw.statusCode)),
			)

			requestCounter.Add(ctx, 1, attrs)
			requestDuration.Record(ctx, time.Since(start).Seconds(), attrs)

			if r.ContentLength > 0 {
				requestSize.Record(ctx, r.ContentLength, attrs)
			}

			if rw.written > 0 {
				responseSize.Record(ctx, rw.written, attrs)
			}
		})
	}
}

// routePattern возвращает шаблон маршрута chi или unmatchedRoute.
func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if pattern := rctx.RoutePattern(); pattern != "" {
			return pattern
		}
	}

	return unmatchedRoute
}
