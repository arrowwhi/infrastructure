package metrics

import (
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

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

// Middleware возвращает middleware для сбора HTTP метрик
func Middleware(serviceName string) func(next http.Handler) http.Handler {
	meter := otel.Meter(serviceName)

	// Создаем метрики
	requestCounter, _ := meter.Int64Counter(
		"http.server.request.count",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("{request}"),
	)

	requestDuration, _ := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithDescription("HTTP request duration"),
		metric.WithUnit("ms"),
	)

	requestSize, _ := meter.Int64Histogram(
		"http.server.request.size",
		metric.WithDescription("HTTP request size"),
		metric.WithUnit("byte"),
	)

	responseSize, _ := meter.Int64Histogram(
		"http.server.response.size",
		metric.WithDescription("HTTP response size"),
		metric.WithUnit("byte"),
	)

	activeRequests, _ := meter.Int64UpDownCounter(
		"http.server.active_requests",
		metric.WithDescription("Number of active HTTP requests"),
		metric.WithUnit("{request}"),
	)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Оборачиваем ResponseWriter для отслеживания статуса и размера
			rw := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Увеличиваем счетчик активных запросов
			activeRequests.Add(r.Context(), 1,
				metric.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.route", r.URL.Path),
				),
			)
			defer func() {
				activeRequests.Add(r.Context(), -1,
					metric.WithAttributes(
						attribute.String("http.method", r.Method),
						attribute.String("http.route", r.URL.Path),
					),
				)
			}()

			// Обрабатываем запрос
			next.ServeHTTP(rw, r)

			// Записываем метрики
			duration := float64(time.Since(start).Milliseconds())
			statusCode := strconv.Itoa(rw.statusCode)

			attrs := []attribute.KeyValue{
				attribute.String("http.method", r.Method),
				attribute.String("http.route", r.URL.Path),
				attribute.String("http.status_code", statusCode),
			}

			requestCounter.Add(r.Context(), 1, metric.WithAttributes(attrs...))
			requestDuration.Record(r.Context(), duration, metric.WithAttributes(attrs...))

			if r.ContentLength > 0 {
				requestSize.Record(r.Context(), r.ContentLength, metric.WithAttributes(attrs...))
			}

			if rw.written > 0 {
				responseSize.Record(r.Context(), rw.written, metric.WithAttributes(attrs...))
			}
		})
	}
}
