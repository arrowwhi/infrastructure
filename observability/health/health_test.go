package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func ok(context.Context) error { return nil }

func TestRegistry_Statuses(t *testing.T) {
	r := New()
	assert.Equal(t, StatusOK, r.Check(context.Background()).Status, "пустой реестр — готов")

	r.Register("db", ok)
	r.Register("cache", func(context.Context) error { return errors.New("down") }, NonCritical())

	report := r.Check(context.Background())
	assert.Equal(t, StatusDegraded, report.Status)
	assert.True(t, report.Ready())
	assert.Equal(t, "down", report.Checks["cache"].Error)

	r.Register("db", func(context.Context) error { return errors.New("no conn") })

	report = r.Check(context.Background())
	assert.Equal(t, StatusFail, report.Status)
	assert.False(t, report.Ready())
	assert.Equal(t, []string{"cache", "db"}, r.Names())
}

func TestRegistry_TimeoutAndPanic(t *testing.T) {
	r := New(WithTimeout(20 * time.Millisecond))
	r.Register("slow", func(ctx context.Context) error {
		<-ctx.Done()

		return ctx.Err()
	})
	r.Register("panics", func(context.Context) error { panic("boom") })

	start := time.Now()
	report := r.Check(context.Background())

	assert.Less(t, time.Since(start), time.Second, "проверки выполняются параллельно и с таймаутом")
	assert.Equal(t, StatusFail, report.Checks["slow"].Status)
	assert.Contains(t, report.Checks["panics"].Error, "panic: boom")
}

func TestReadinessHandler(t *testing.T) {
	r := New()
	r.Register("db", func(context.Context) error { return errors.New("down") })

	rec := httptest.NewRecorder()
	r.ReadinessHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var report Report
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&report))
	assert.Equal(t, StatusFail, report.Status)

	rec = httptest.NewRecorder()
	LivenessHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code, "liveness не зависит от зависимостей")
}

func TestRegisterMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	r := New()
	r.Register("db", func(context.Context) error { return errors.New("down") })
	require.NoError(t, r.RegisterMetrics(mp.Meter("test")))
	r.Check(context.Background())

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	var found bool

	for _, m := range rm.ScopeMetrics[0].Metrics {
		if m.Name != "health.check.status" {
			continue
		}

		gauge := m.Data.(metricdata.Gauge[int64]) //nolint:forcetypeassert
		require.Len(t, gauge.DataPoints, 1)
		assert.Equal(t, int64(0), gauge.DataPoints[0].Value)

		found = true
	}

	assert.True(t, found)
}
