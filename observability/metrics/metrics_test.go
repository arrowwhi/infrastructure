package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// useManualReader подменяет глобальный MeterProvider на провайдер с ручным ридером.
func useManualReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)

	t.Cleanup(func() {
		otel.SetMeterProvider(prev)
		_ = mp.Shutdown(context.Background())
	})

	return reader
}

func collect(t *testing.T, reader *sdkmetric.ManualReader, name string) metricdata.Metrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m
			}
		}
	}

	t.Fatalf("metric %q not found", name)

	return metricdata.Metrics{}
}

func attrValue(set attribute.Set, key string) string {
	v, _ := set.Value(attribute.Key(key))

	return v.AsString()
}

func TestMiddleware_UsesRoutePatternAndSeconds(t *testing.T) {
	reader := useManualReader(t)

	r := chi.NewRouter()
	r.Use(Middleware("test"))
	r.Get("/users/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	for _, id := range []string{"1", "2", "3"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/"+id, nil))
	}

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nope/42", nil))

	m := collect(t, reader, "http.server.request.duration")
	if m.Unit != "s" {
		t.Errorf("unit = %q, want s", m.Unit)
	}

	hist, ok := m.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("unexpected data type %T", m.Data)
	}

	routes := map[string]uint64{}
	for _, dp := range hist.DataPoints {
		routes[attrValue(dp.Attributes, "http.route")] += dp.Count
	}

	if routes["/users/{id}"] != 3 {
		t.Errorf("route /users/{id} count = %d, want 3 (routes: %v)", routes["/users/{id}"], routes)
	}

	if routes[unmatchedRoute] != 1 {
		t.Errorf("unmatched route count = %d, want 1 (routes: %v)", routes[unmatchedRoute], routes)
	}

	if len(routes) != 2 {
		t.Errorf("raw paths must not leak into labels, got %v", routes)
	}
}

func TestUnaryClientInterceptor(t *testing.T) {
	reader := useManualReader(t)

	interceptor := UnaryClientInterceptor("test")

	failing := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		time.Sleep(2 * time.Millisecond)

		return status.Error(codes.Unavailable, "down")
	}

	_ = interceptor(context.Background(), "/svc/Method", nil, nil, nil, failing)

	m := collect(t, reader, "grpc.client.request.count")

	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 {
		t.Fatalf("unexpected data %#v", m.Data)
	}

	dp := sum.DataPoints[0]
	if got := attrValue(dp.Attributes, "grpc.status"); got != codes.Unavailable.String() {
		t.Errorf("grpc.status = %q", got)
	}

	hist := collect(t, reader, "grpc.client.request.duration").Data.(metricdata.Histogram[float64]) //nolint:forcetypeassert
	if hist.DataPoints[0].Sum < 0.002 || hist.DataPoints[0].Sum > 1 {
		t.Errorf("duration must be in seconds, got %v", hist.DataPoints[0].Sum)
	}
}

func TestBuildInfo(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	if err := registerBuildInfo(mp, "svc", "1.2.3", "prod"); err != nil {
		t.Fatal(err)
	}

	m := collect(t, reader, BuildInfoMetric)

	gauge, ok := m.Data.(metricdata.Gauge[int64])
	if !ok || len(gauge.DataPoints) != 1 || gauge.DataPoints[0].Value != 1 {
		t.Fatalf("unexpected data %#v", m.Data)
	}

	if v := attrValue(gauge.DataPoints[0].Attributes, "version"); v != "1.2.3" {
		t.Errorf("version = %q", v)
	}
}
