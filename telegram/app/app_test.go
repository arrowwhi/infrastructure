package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfig() Config {
	var cfg Config

	cfg.ServiceName = "test"
	cfg.ShutdownTimeout = time.Second
	cfg.Logger.Level = "error"
	cfg.Metrics.Enabled = false // Prometheus-экспортер регистрируется глобально один раз

	return cfg
}

func TestRun_ShutdownOrder(t *testing.T) {
	a, err := New(context.Background(), testConfig())
	require.NoError(t, err)

	var order []string

	a.OnShutdown(func(context.Context) error { order = append(order, "first"); return nil })
	a.OnShutdown(func(context.Context) error { order = append(order, "second"); return nil })

	ctx, cancel := context.WithCancel(context.Background())

	stopped := make(chan struct{})
	component := ComponentFunc(func(ctx context.Context) error {
		<-ctx.Done()
		close(stopped)

		return nil
	})

	done := make(chan error, 1)

	go func() { done <- a.Run(ctx, component) }()

	cancel()

	select {
	case err = <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}

	<-stopped
	assert.Equal(t, []string{"second", "first"}, order, "OnShutdown вызывается в обратном порядке")
}

func TestRun_ComponentErrorStopsOthers(t *testing.T) {
	a, err := New(context.Background(), testConfig())
	require.NoError(t, err)

	boom := errors.New("boom")
	otherStopped := make(chan struct{})

	err = a.Run(context.Background(),
		ComponentFunc(func(context.Context) error { return boom }),
		ComponentFunc(func(ctx context.Context) error { <-ctx.Done(); close(otherStopped); return nil }),
	)

	require.ErrorIs(t, err, boom)
	<-otherStopped
}

func TestAdminHandler(t *testing.T) {
	a, err := New(context.Background(), testConfig())
	require.NoError(t, err)

	a.Health().Register("db", func(context.Context) error { return errors.New("down") })

	rec := httptest.NewRecorder()
	a.AdminHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	rec = httptest.NewRecorder()
	a.AdminHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLoadConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SERVICE_NAME", "svc")
	t.Setenv("LOG_LEVEL", "warn")

	var cfg struct {
		App Config
	}

	require.NoError(t, LoadConfig(&cfg))
	assert.Equal(t, "svc", cfg.App.ServiceName)
	assert.Equal(t, "warn", cfg.App.Logger.Level)
	assert.Equal(t, "9090", cfg.App.Admin.Port)
	assert.Equal(t, 30*time.Second, cfg.App.ShutdownTimeout)
}

func TestLoadConfig_RequiresServiceName(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SERVICE_NAME", "")
	require.NoError(t, os.Unsetenv("SERVICE_NAME")) // t.Setenv восстановит значение после теста

	var cfg Config
	require.Error(t, LoadConfig(&cfg))
}
