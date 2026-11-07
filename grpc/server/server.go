// Package server provides a gRPC server with recovery, logging, OpenTelemetry metrics and tracing,
// health checking, reflection, graceful shutdown and an optional grpc-gateway HTTP/JSON proxy.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"github.com/arrowwhi/infrastructure/observability/health"
	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/arrowwhi/infrastructure/observability/metrics"
)

const defaultHealthInterval = 10 * time.Second

// Service — реализация gRPC-сервиса, которую можно зарегистрировать на сервере.
// Обычно это обёртка над сгенерированным pb.RegisterXxxServer.
type Service interface {
	RegisterGRPC(s *grpc.Server)
}

// GatewayService — сервис, который дополнительно публикуется через grpc-gateway.
// Обычно это обёртка над сгенерированным pb.RegisterXxxHandler.
type GatewayService interface {
	RegisterGateway(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error
}

// Server — gRPC-сервер с инфраструктурной обвязкой.
type Server struct {
	grpcServer    *grpc.Server
	health        *grpchealth.Server
	gatewayServer *http.Server
	gatewayConn   *grpc.ClientConn
	listener      net.Listener

	serviceName string
	services    []Service

	healthRegistry *health.Registry
	healthInterval time.Duration

	unary         []grpc.UnaryServerInterceptor
	stream        []grpc.StreamServerInterceptor
	serverOpts    []grpc.ServerOption
	gwMuxOpts     []runtime.ServeMuxOption
	gwMiddlewares []func(http.Handler) http.Handler

	cfg Config

	shutdownOnce sync.Once
	shutdownErr  error
}

// New собирает gRPC-сервер. Сервисы регистрируются опцией WithServices или методом Register
// до вызова Run. serviceName используется в метриках и трейсах.
func New(cfg Config, serviceName string, opts ...Option) (*Server, error) {
	if serviceName == "" {
		return nil, errors.New("grpc server: service name is required")
	}

	s := &Server{cfg: cfg, serviceName: serviceName}

	for _, opt := range opts {
		opt(s)
	}

	s.grpcServer = grpc.NewServer(s.buildServerOptions()...)

	for _, svc := range s.services {
		svc.RegisterGRPC(s.grpcServer)
	}

	if cfg.HealthEnabled {
		s.health = grpchealth.NewServer()
		healthpb.RegisterHealthServer(s.grpcServer, s.health)
	}

	if cfg.ReflectionEnabled {
		reflection.Register(s.grpcServer)
	}

	return s, nil
}

// Register регистрирует сервис. Вызывать до Run.
func (s *Server) Register(svc Service) {
	s.services = append(s.services, svc)
	svc.RegisterGRPC(s.grpcServer)
}

// GRPC возвращает нижележащий *grpc.Server — для регистрации сгенерированных сервисов напрямую.
func (s *Server) GRPC() *grpc.Server {
	return s.grpcServer
}

// Health возвращает health-сервер (nil, если HealthEnabled=false). Через него можно
// переводить отдельные сервисы в NOT_SERVING, например при потере зависимости.
func (s *Server) Health() *grpchealth.Server {
	return s.health
}

// Run запускает gRPC-сервер (и шлюз, если включён) и блокируется до отмены ctx или ошибки.
// При отмене ctx выполняется graceful shutdown.
func (s *Server) Run(ctx context.Context) error {
	if s.listener == nil {
		lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.cfg.Addr())
		if err != nil {
			return fmt.Errorf("listen %s: %w", s.cfg.Addr(), err)
		}

		s.listener = lis
	}

	if s.cfg.Gateway.Enabled {
		if err := s.setupGateway(ctx); err != nil {
			_ = s.listener.Close()

			return err
		}
	}

	s.markServing()

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		logger.Info(gCtx).Str("addr", s.listener.Addr().String()).Msg("Starting gRPC server")

		if err := s.grpcServer.Serve(s.listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return fmt.Errorf("grpc serve: %w", err)
		}

		return nil
	})

	if s.gatewayServer != nil {
		g.Go(func() error {
			logger.Info(gCtx).Str("addr", s.gatewayServer.Addr).Msg("Starting gRPC gateway")

			if err := s.gatewayServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("gateway serve: %w", err)
			}

			return nil
		})
	}

	if s.health != nil && s.healthRegistry != nil {
		g.Go(func() error {
			s.watchHealth(gCtx)

			return nil
		})
	}

	g.Go(func() error {
		<-gCtx.Done()

		return s.Shutdown(context.WithoutCancel(ctx))
	})

	return g.Wait()
}

// Shutdown переводит health в NOT_SERVING, останавливает шлюз и gRPC-сервер.
// Активные RPC дожидаются завершения не дольше ShutdownTimeout. Повторный вызов безопасен.
func (s *Server) Shutdown(ctx context.Context) error {
	s.shutdownOnce.Do(func() {
		s.shutdownErr = s.shutdown(ctx)
	})

	return s.shutdownErr
}

func (s *Server) shutdown(ctx context.Context) error {
	if s.cfg.ShutdownTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.cfg.ShutdownTimeout)

		defer cancel()
	}

	logger.Info(ctx).Msg("Shutting down gRPC server")

	if s.health != nil {
		s.health.Shutdown()
	}

	var errs []error

	if s.gatewayServer != nil {
		if err := s.gatewayServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("gateway shutdown: %w", err))
		}
	}

	if s.gatewayConn != nil {
		if err := s.gatewayConn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("gateway conn close: %w", err))
		}
	}

	stopped := make(chan struct{})

	go func() {
		s.grpcServer.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-ctx.Done():
		logger.Warn(ctx).Msg("gRPC graceful stop timed out, forcing stop")
		s.grpcServer.Stop()
		<-stopped
	}

	logger.Info(ctx).Msg("gRPC server stopped")

	return errors.Join(errs...)
}

// markServing выставляет SERVING для сервера целиком и для каждого зарегистрированного сервиса.
func (s *Server) markServing() {
	s.setServingStatus(healthpb.HealthCheckResponse_SERVING)
}

func (s *Server) setServingStatus(st healthpb.HealthCheckResponse_ServingStatus) {
	if s.health == nil {
		return
	}

	s.health.SetServingStatus("", st)

	for name := range s.grpcServer.GetServiceInfo() {
		s.health.SetServingStatus(name, st)
	}
}

// watchHealth переводит gRPC health в SERVING/NOT_SERVING по результатам реестра проверок.
func (s *Server) watchHealth(ctx context.Context) {
	interval := s.healthInterval
	if interval <= 0 {
		interval = defaultHealthInterval
	}

	s.healthRegistry.Watch(ctx, interval, func(report health.Report) {
		if ctx.Err() != nil {
			return
		}

		if report.Ready() {
			s.setServingStatus(healthpb.HealthCheckResponse_SERVING)

			return
		}

		logger.Warn(ctx).Interface("checks", report.Checks).Msg("Health checks failed, gRPC server is NOT_SERVING")
		s.setServingStatus(healthpb.HealthCheckResponse_NOT_SERVING)
	})
}

func (s *Server) buildServerOptions() []grpc.ServerOption {
	unary := []grpc.UnaryServerInterceptor{
		recovery.UnaryServerInterceptor(recovery.WithRecoveryHandlerContext(recoveryHandler)),
	}
	stream := []grpc.StreamServerInterceptor{
		recovery.StreamServerInterceptor(recovery.WithRecoveryHandlerContext(recoveryHandler)),
	}

	if s.cfg.LoggingEnabled {
		logOpts := []logging.Option{
			logging.WithLogOnEvents(logging.FinishCall),
		}
		unary = append(unary, logging.UnaryServerInterceptor(interceptorLogger(), logOpts...))
		stream = append(stream, logging.StreamServerInterceptor(interceptorLogger(), logOpts...))
	}

	if s.cfg.MetricsEnabled {
		unary = append(unary, metrics.UnaryServerInterceptor(s.serviceName))
		stream = append(stream, metrics.StreamServerInterceptor(s.serviceName))
	}

	unary = append(unary, s.unary...)
	stream = append(stream, s.stream...)

	opts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(unary...),
		grpc.ChainStreamInterceptor(stream...),
	}

	if s.cfg.TracingEnabled {
		opts = append(opts, grpc.StatsHandler(otelgrpc.NewServerHandler()))
	}

	if s.cfg.MaxRecvMsgSize > 0 {
		opts = append(opts, grpc.MaxRecvMsgSize(s.cfg.MaxRecvMsgSize))
	}

	if s.cfg.MaxSendMsgSize > 0 {
		opts = append(opts, grpc.MaxSendMsgSize(s.cfg.MaxSendMsgSize))
	}

	return append(opts, s.serverOpts...)
}

// setupGateway подключается к собственному gRPC-листенеру и регистрирует HTTP-обработчики.
func (s *Server) setupGateway(ctx context.Context) error {
	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if s.cfg.TracingEnabled {
		dialOpts = append(dialOpts, grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	}

	conn, err := grpc.NewClient(dialTarget(s.listener.Addr()), dialOpts...)
	if err != nil {
		return fmt.Errorf("gateway dial: %w", err)
	}

	mux := runtime.NewServeMux(s.gwMuxOpts...)

	for _, svc := range s.services {
		gw, ok := svc.(GatewayService)
		if !ok {
			continue
		}

		if err = gw.RegisterGateway(ctx, mux, conn); err != nil {
			_ = conn.Close()

			return fmt.Errorf("gateway register: %w", err)
		}
	}

	var handler http.Handler = mux

	for i := len(s.gwMiddlewares) - 1; i >= 0; i-- {
		handler = s.gwMiddlewares[i](handler)
	}

	// HTTP-метрики на шлюзе не пишем: каждый запрос шлюза — это gRPC-вызов, который
	// уже учтён серверным интерсептором с меткой метода. Трейс же нужен, чтобы
	// связать HTTP-запрос с gRPC-спаном.
	if s.cfg.TracingEnabled {
		handler = otelhttp.NewHandler(handler, s.serviceName+".gateway")
	}

	s.gatewayConn = conn
	s.gatewayServer = &http.Server{
		Addr:              s.cfg.Gateway.Addr(),
		Handler:           handler,
		ReadHeaderTimeout: s.cfg.Gateway.ReadHeaderTimeout,
	}

	return nil
}

// dialTarget превращает адрес листенера в адрес для подключения: 0.0.0.0/:: → 127.0.0.1.
func dialTarget(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok || tcp.IP == nil || tcp.IP.IsUnspecified() {
		port := "0"
		if ok {
			port = fmt.Sprint(tcp.Port)
		}

		return net.JoinHostPort("127.0.0.1", port)
	}

	return tcp.String()
}

func recoveryHandler(ctx context.Context, p any) error {
	logger.Error(ctx).
		Interface("panic", p).
		Bytes("stack", debug.Stack()).
		Msg("gRPC handler panic recovered")

	return status.Error(codes.Internal, "internal error")
}
