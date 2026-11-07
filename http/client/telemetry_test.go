package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestRetry_ResendsBody: повтор POST должен уходить с тем же телом, а не с пустым.
func TestRetry_ResendsBody(t *testing.T) {
	var (
		mu     sync.Mutex
		bodies []string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)

		mu.Lock()
		bodies = append(bodies, string(b))
		attempt := len(bodies)
		mu.Unlock()

		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	cli, err := NewClient(Config{
		BaseURL:        srv.URL,
		MetricsEnabled: true,
		Retry:          &RetryConfig{MaxRetries: 2, BackoffTime: time.Millisecond},
	})
	require.NoError(t, err)

	resp, err := cli.Post(context.Background(), "/").
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{"k": "v"}).
		Execute(nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, bodies, 2)
	assert.JSONEq(t, `{"k":"v"}`, bodies[0])
	assert.Equal(t, bodies[0], bodies[1], "повтор должен отправить то же тело")
}

// TestTracing_PropagatesTraceparent: при включённом трейсинге клиент передаёт trace context.
func TestTracing_PropagatesTraceparent(t *testing.T) {
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()

	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	gotHeader := make(chan string, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader <- r.Header.Get("Traceparent")
	}))
	defer srv.Close()

	cli, err := NewClient(Config{BaseURL: srv.URL, TracingEnabled: true})
	require.NoError(t, err)

	ctx, span := tp.Tracer("test").Start(context.Background(), "parent")
	defer span.End()

	_, err = cli.Get(ctx, "/").Execute(nil)
	require.NoError(t, err)

	header := <-gotHeader
	require.NotEmpty(t, header)
	assert.Contains(t, header, span.SpanContext().TraceID().String())
}
