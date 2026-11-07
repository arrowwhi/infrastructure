// Package client provides a gRPC client with support for TLS, retries, logging, and keepalive.
package client

import (
	"context"
	"fmt"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/retry"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

func NewClient(cfg *Config, logger *zerolog.Logger) (*grpc.ClientConn, error) {
	var dialOpts []grpc.DialOption

	if cfg.EnableLogging {
		dialOpts = append(dialOpts, grpc.WithUnaryInterceptor(logging.UnaryClientInterceptor(interceptorLogger(logger))))
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

	if cfg.EnableRetries {
		dialOpts = append(dialOpts, grpc.WithUnaryInterceptor(createRetryInterceptor(cfg.Retry)))
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

func createRetryInterceptor(cfg *RetryConfig) grpc.UnaryClientInterceptor {
	retryCodes := []codes.Code{
		codes.Unavailable,
		codes.DeadlineExceeded,
		codes.ResourceExhausted,
		codes.Aborted,
		codes.Internal,

		// codes.Unknown,
		// codes.DataLoss,
	}

	retryOpts := []retry.CallOption{
		retry.WithMax(5),
		retry.WithBackoff(retry.BackoffLinearWithJitter(cfg.BackoffTime, cfg.BackoffFraction)),
		retry.WithCodes(retryCodes...),
		retry.WithOnRetryCallback(func(ctx context.Context, attempt uint, err error) {
			logger := log.Ctx(ctx)
			logger.Warn().
				Uint("attempt", attempt).
				Str("error", err.Error()).
				Str("code", status.Code(err).String()).
				Msg("gRPC retry attempt")
		}),
	}

	return retry.UnaryClientInterceptor(retryOpts...)
}

func createKeepAliveParams(cfg KeepAliveConfig) grpc.DialOption {
	return grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                cfg.RefreshTime,
		Timeout:             cfg.Timeout,
		PermitWithoutStream: cfg.PermitWithoutStream,
	})
}
