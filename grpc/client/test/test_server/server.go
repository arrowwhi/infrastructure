// Package test provides test utilities for gRPC client testing including a test server implementation.
package test

import (
	"context"
	"net"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/arrowwhi/infrastructure/grpc/client/test/test_server/gen"
)

// TestService - простой тестовый сервис
type TestService struct {
	gen.UnimplementedTestServer
	logger     *zerolog.Logger
	delay      time.Duration
	shouldFail bool
	calls      atomic.Int64
}

// NewTestService создает новый тестовый сервис
func NewTestService(logger *zerolog.Logger) *TestService {
	return &TestService{
		logger: logger,
	}
}

func (s *TestService) Ping(ctx context.Context, req *emptypb.Empty) (*gen.PingResponse, error) {
	s.calls.Add(1)

	if s.delay > 0 {
		time.Sleep(s.delay)
	}

	if s.shouldFail {
		return nil, status.Error(codes.Internal, "test error")
	}

	return &gen.PingResponse{Message: "pong"}, nil
}

func (s *TestService) SlowPing(ctx context.Context, req *emptypb.Empty) (*gen.PingResponse, error) {
	time.Sleep(2 * time.Second)
	return &gen.PingResponse{Message: "slow pong"}, nil
}

func (s *TestService) FailingPing(ctx context.Context, req *emptypb.Empty) (*gen.PingResponse, error) {
	return nil, status.Error(codes.Internal, "always fails")
}

// Calls возвращает число вызовов Ping (нужно тестам ретраев).
func (s *TestService) Calls() int64 {
	return s.calls.Load()
}

func (s *TestService) SetShouldFail(fail bool) {
	s.shouldFail = fail
}

func (s *TestService) SetDelay(delay time.Duration) {
	s.delay = delay
}

// TestGRPCServer - тестовый gRPC сервер
type TestGRPCServer struct {
	server   *grpc.Server
	listener net.Listener
	service  *TestService
}

// NewTestServer создает новый тестовый сервер
func NewTestServer(useTLS bool, certFile, keyFile string, logger *zerolog.Logger) (*TestGRPCServer, error) {
	lc := net.ListenConfig{}
	lis, err := lc.Listen(context.Background(), "tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	var opts []grpc.ServerOption
	if useTLS {
		creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.Creds(creds))
	}

	server := grpc.NewServer(opts...)
	service := NewTestService(logger)
	gen.RegisterTestServer(server, service)

	return &TestGRPCServer{
		server:   server,
		listener: lis,
		service:  service,
	}, nil
}

func (ts *TestGRPCServer) Start() error {
	go func() {
		_ = ts.server.Serve(ts.listener)
	}()

	return nil
}

func (ts *TestGRPCServer) Stop() {
	ts.server.Stop()
	_ = ts.listener.Close()
}

func (ts *TestGRPCServer) Addr() string {
	return ts.listener.Addr().String()
}

func (ts *TestGRPCServer) Service() *TestService {
	return ts.service
}
