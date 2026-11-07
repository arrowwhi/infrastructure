// Package client provides a gRPC client with support for TLS, retries, logging, keepalive,
// OpenTelemetry metrics and tracing.
package client

import (
	"context"
	"fmt"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/retry"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	obslog "github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/arrowwhi/infrastructure/observability/metrics"
)

const defaultServiceName = "grpc-client"

// NewClient создаёт соединение с настроенной цепочкой интерсепторов.
//
// Порядок unary-интерсепторов (снаружи внутрь): таймаут вызова → логирование →
// метрики → ретраи. Логи и метрики описывают логический вызов целиком (итоговый
// статус и полную длительность), а каждая попытка видна отдельным спаном otelgrpc
// и счётчиком grpc.client.retry.count. logger может быть nil.
func NewClient(cfg *Config, logger *zerolog.Logger) (*grpc.ClientConn, error) {
	if logger == nil {
		nop := zerolog.Nop()
		logger = &nop
	}

	// trace_id/span_id в логах клиента: интерсептор передаёт контекст вызова в событие.
	traced := logger.Hook(obslog.TraceHook{})
	logger = &traced

	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = defaultServiceName
	}

	var (
		dialOpts []grpc.DialOption
		unary    []grpc.UnaryClientInterceptor
		stream   []grpc.StreamClientInterceptor
	)

	if cfg.Timeout > 0 {
		unary = append(unary, defaultTimeoutInterceptor(cfg))
	}

	if cfg.EnableLogging {
		unary = append(unary, logging.UnaryClientInterceptor(interceptorLogger(logger)))
		stream = append(stream, logging.StreamClientInterceptor(interceptorLogger(logger)))
	}

	if cfg.EnableMetrics {
		unary = append(unary, metrics.UnaryClientInterceptor(serviceName))
		stream = append(stream, metrics.StreamClientInterceptor(serviceName))
	}

	if cfg.EnableRetries {
		retryInterceptor, err := createRetryInterceptor(cfg.Retry, logger, serviceName)
		if err != nil {
			return nil, err
		}

		unary = append(unary, retryInterceptor)
	}

	// Все интерсепторы — одной цепочкой: несколько grpc.WithUnaryInterceptor
	// перезаписывают друг друга, и в силе остаётся только последний.
	dialOpts = append(dialOpts,
		grpc.WithChainUnaryInterceptor(unary...),
		grpc.WithChainStreamInterceptor(stream...),
	)

	if cfg.EnableTracing {
		dialOpts = append(dialOpts, grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	}

	if cfg.EnableTLS {
		creds, err := buildTransportCredentials(cfg.TLSConfig)
		if err != nil {
			return nil, err
		}

		dialOpts = append(dialOpts, grpc.WithTransportCredentials(creds))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	if cfg.EnableKeepAlive && cfg.KeepAlive != nil {
		dialOpts = append(dialOpts, createKeepAliveParams(*cfg.KeepAlive))
	}

	conn, err := grpc.NewClient(cfg.Endpoints, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	return conn, nil
}

// defaultTimeoutInterceptor ставит дедлайн cfg.Timeout, если вызывающий его не задал.
func defaultTimeoutInterceptor(cfg *Config) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)

			defer cancel()
		}

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func createRetryInterceptor(cfg *RetryConfig, logger *zerolog.Logger, serviceName string) (grpc.UnaryClientInterceptor, error) {
	if cfg == nil {
		cfg = &RetryConfig{}
	}

	retryCounter, err := otel.Meter(serviceName).Int64Counter(
		"grpc.client.retry.count",
		metric.WithDescription("Number of gRPC client retry attempts"),
		metric.WithUnit("{retry}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create retry counter: %w", err)
	}

	retryCodes := []codes.Code{
		codes.Unavailable,
		codes.DeadlineExceeded,
		codes.ResourceExhausted,
		codes.Aborted,
		codes.Internal,
	}

	retryOpts := []retry.CallOption{
		// retry.WithMax задаёт общее число попыток, включая первую.
		retry.WithMax(cfg.MaxRetries + 1),
		retry.WithBackoff(retry.BackoffLinearWithJitter(cfg.BackoffTime, cfg.BackoffFraction)),
		retry.WithCodes(retryCodes...),
		retry.WithOnRetryCallback(func(ctx context.Context, attempt uint, err error) {
			code := status.Code(err).String()

			retryCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("grpc.status", code)))

			logger.Warn().
				Ctx(ctx).
				Uint("attempt", attempt).
				Str("error", err.Error()).
				Str("code", code).
				Msg("gRPC retry attempt")
		}),
	}

	if cfg.PerRetryTimeout > 0 {
		retryOpts = append(retryOpts, retry.WithPerRetryTimeout(cfg.PerRetryTimeout))
	}

	return retry.UnaryClientInterceptor(retryOpts...), nil
}

func createKeepAliveParams(cfg KeepAliveConfig) grpc.DialOption {
	return grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                cfg.RefreshTime,
		Timeout:             cfg.Timeout,
		PermitWithoutStream: cfg.PermitWithoutStream,
	})
}
