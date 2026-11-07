package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/arrowwhi/infrastructure/observability/health"
)

func startServer(t *testing.T, cfg Config, opts ...Option) (*Server, string, context.CancelFunc, <-chan error) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv, err := New(cfg, "test-service", append(opts, WithListener(lis))...)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- srv.Run(ctx) }()

	return srv, lis.Addr().String(), cancel, done
}

func dial(t *testing.T, addr string) *grpc.ClientConn {
	t.Helper()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

func waitServing(t *testing.T, client healthpb.HealthClient) {
	t.Helper()

	require.Eventually(t, func() bool {
		resp, err := client.Check(context.Background(), &healthpb.HealthCheckRequest{})

		return err == nil && resp.GetStatus() == healthpb.HealthCheckResponse_SERVING
	}, 3*time.Second, 20*time.Millisecond)
}

func TestServer_HealthAndGracefulShutdown(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ShutdownTimeout = time.Second

	_, addr, cancel, done := startServer(t, cfg)
	client := healthpb.NewHealthClient(dial(t, addr))

	waitServing(t, client)

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestServer_RecoversPanic(t *testing.T) {
	panicky := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		panic("boom")
	}

	_, addr, cancel, done := startServer(t, DefaultConfig(), WithUnaryInterceptors(panicky))
	defer func() { cancel(); <-done }()

	_, err := healthpb.NewHealthClient(dial(t, addr)).Check(context.Background(), &healthpb.HealthCheckRequest{})
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestServer_Gateway(t *testing.T) {
	gwLis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	_, gwPort, _ := net.SplitHostPort(gwLis.Addr().String())
	require.NoError(t, gwLis.Close())

	cfg := DefaultConfig()
	cfg.Gateway.Enabled = true
	cfg.Gateway.Host = "127.0.0.1"
	cfg.Gateway.Port = gwPort

	_, _, cancel, done := startServer(t, cfg)
	defer func() { cancel(); <-done }()

	// Сервисов со шлюзом нет — мультиплексор отвечает 404, но сам шлюз должен подняться.
	require.Eventually(t, func() bool {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:"+gwPort+"/unknown", nil)

		resp, reqErr := http.DefaultClient.Do(req)
		if reqErr != nil {
			return false
		}

		_ = resp.Body.Close()

		return resp.StatusCode == http.StatusNotFound
	}, 3*time.Second, 20*time.Millisecond)
}

func TestNew_RequiresServiceName(t *testing.T) {
	_, err := New(DefaultConfig(), "")
	require.Error(t, err)
}

func TestServer_HealthRegistryDrivesServingStatus(t *testing.T) {
	var healthy atomic.Bool

	healthy.Store(true)

	registry := health.New()
	registry.Register("db", func(context.Context) error {
		if healthy.Load() {
			return nil
		}

		return errors.New("db down")
	})

	_, addr, cancel, done := startServer(t, DefaultConfig(), WithHealthRegistry(registry, 20*time.Millisecond))
	defer func() { cancel(); <-done }()

	client := healthpb.NewHealthClient(dial(t, addr))
	waitServing(t, client)

	healthy.Store(false)

	require.Eventually(t, func() bool {
		resp, err := client.Check(context.Background(), &healthpb.HealthCheckRequest{})

		return err == nil && resp.GetStatus() == healthpb.HealthCheckResponse_NOT_SERVING
	}, 3*time.Second, 20*time.Millisecond)

	healthy.Store(true)
	waitServing(t, client)
}
