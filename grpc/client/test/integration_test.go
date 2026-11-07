package test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/arrowwhi/infrastructure/grpc/client"
	. "github.com/arrowwhi/infrastructure/grpc/client/test/test_server"
	"github.com/arrowwhi/infrastructure/grpc/client/test/test_server/gen"
)

func TestIntegration_BasicConnection(t *testing.T) {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	tests := []struct {
		name     string
		expected string
		useTLS   bool
	}{
		{
			name:     "Basic connection without TLS",
			useTLS:   false,
			expected: "pong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := NewTestServer(tt.useTLS, "", "", &logger)
			require.NoError(t, err, "Failed to create test server")
			defer server.Stop()

			err = server.Start()
			require.NoError(t, err, "Failed to start test server")

			cfg := &client.Config{
				Endpoints:     server.Addr(),
				Timeout:       5 * time.Second,
				EnableLogging: true,
				EnableTLS:     tt.useTLS,
				EnableRetries: false,
			}

			conn, err := client.NewClient(cfg, &logger)
			require.NoError(t, err, "Failed to create client")
			defer conn.Close()

			testClient := gen.NewTestClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := testClient.Ping(ctx, &emptypb.Empty{})
			require.NoError(t, err, "Ping failed")
			assert.Equal(t, tt.expected, resp.Message, "Expected correct response")
		})
	}
}

func TestIntegration_TLSConnection(t *testing.T) {
	// Создаем временную директорию для сертификатов
	tempDir, err := os.MkdirTemp("", "grpc_test_certs")
	require.NoError(t, err, "Failed to create temp dir")
	defer os.RemoveAll(tempDir)

	// Генерируем тестовые сертификаты
	certs, err := generateTestCerts(tempDir)
	require.NoError(t, err, "Failed to generate test certificates")

	// Настройка логирования
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	// Запуск тестового сервера с TLS
	server, err := NewTestServer(true, certs.CertFile, certs.KeyFile, &logger)
	require.NoError(t, err, "Failed to create TLS test server")
	defer server.Stop()

	err = server.Start()
	require.NoError(t, err, "Failed to start TLS test server")

	// Конфигурация клиента с TLS
	cfg := &client.Config{
		Endpoints:     server.Addr(),
		Timeout:       5 * time.Second,
		EnableLogging: true,
		EnableTLS:     true,
		TLSConfig: &client.TLSConfig{
			CAFile: certs.CAFile,
		},
		EnableRetries: false,
	}

	// Создание клиента
	conn, err := client.NewClient(cfg, &logger)
	require.NoError(t, err, "Failed to create TLS client")
	defer conn.Close()

	// Тест TLS соединения
	testClient := gen.NewTestClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := testClient.Ping(ctx, &emptypb.Empty{})
	require.NoError(t, err, "TLS Ping failed")
	assert.Equal(t, "pong", resp.Message, "Expected 'pong' response")
}

func TestIntegration_RetryMechanism(t *testing.T) {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	tests := []struct {
		name        string
		maxRetries  uint
		errorCode   codes.Code
		serverFail  bool
		expectError bool
	}{
		{
			name:        "Server fails with retry",
			serverFail:  true,
			maxRetries:  3,
			expectError: true,
			errorCode:   codes.Internal,
		},
		{
			name:        "Server succeeds with retry enabled",
			serverFail:  false,
			maxRetries:  3,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := NewTestServer(false, "", "", &logger)
			require.NoError(t, err, "Failed to create test server")
			defer server.Stop()

			err = server.Start()
			require.NoError(t, err, "Failed to start test server")

			server.Service().SetShouldFail(tt.serverFail)

			cfg := &client.Config{
				Endpoints:     server.Addr(),
				Timeout:       10 * time.Second,
				EnableTLS:     false,
				EnableRetries: true,
				Retry: &client.RetryConfig{
					MaxRetries:      tt.maxRetries,
					BackoffTime:     100 * time.Millisecond,
					BackoffFraction: 0.1,
				},
			}

			conn, err := client.NewClient(cfg, &logger)
			require.NoError(t, err, "Failed to create client")
			defer conn.Close()

			testClient := gen.NewTestClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			_, err = testClient.Ping(ctx, &emptypb.Empty{})
			if tt.expectError {
				require.Error(t, err, "Expected error due to server failure")
				assert.Equal(t, tt.errorCode, status.Code(err), "Expected correct error code")
			} else {
				require.NoError(t, err, "Expected success")
			}
		})
	}
}

func TestIntegration_KeepAlive(t *testing.T) {
	// Настройка логирования
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	// Запуск тестового сервера
	server, err := NewTestServer(false, "", "", &logger)
	require.NoError(t, err, "Failed to create test server")
	defer server.Stop()

	err = server.Start()
	require.NoError(t, err, "Failed to start test server")

	// Конфигурация клиента с keep-alive
	cfg := &client.Config{
		Endpoints:       server.Addr(),
		Timeout:         5 * time.Second,
		EnableTLS:       false,
		EnableRetries:   false,
		EnableKeepAlive: true,
		KeepAlive: &client.KeepAliveConfig{
			RefreshTime:         30 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: true,
		},
	}

	// Создание клиента
	conn, err := client.NewClient(cfg, &logger)
	require.NoError(t, err, "Failed to create client")
	defer conn.Close()

	// Тест keep-alive соединения
	testClient := gen.NewTestClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := testClient.Ping(ctx, &emptypb.Empty{})
	require.NoError(t, err, "Keep-alive Ping failed")
	assert.Equal(t, "pong", resp.Message, "Expected 'pong' response")
}

func TestIntegration_Timeout(t *testing.T) {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	tests := []struct {
		name          string
		serverDelay   time.Duration
		clientTimeout time.Duration
		expectError   bool
		errorCode     codes.Code
	}{
		{
			name:          "Client timeout shorter than server delay",
			serverDelay:   2 * time.Second,
			clientTimeout: 1 * time.Second,
			expectError:   true,
			errorCode:     codes.DeadlineExceeded,
		},
		{
			name:          "Client timeout longer than server delay",
			serverDelay:   500 * time.Millisecond,
			clientTimeout: 2 * time.Second,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := NewTestServer(false, "", "", &logger)
			require.NoError(t, err, "Failed to create test server")
			defer server.Stop()

			err = server.Start()
			require.NoError(t, err, "Failed to start test server")

			server.Service().SetDelay(tt.serverDelay)

			cfg := &client.Config{
				Endpoints:     server.Addr(),
				Timeout:       tt.clientTimeout,
				EnableTLS:     false,
				EnableRetries: false,
			}

			conn, err := client.NewClient(cfg, &logger)
			require.NoError(t, err, "Failed to create client")
			defer conn.Close()

			testClient := gen.NewTestClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), tt.clientTimeout)
			defer cancel()

			_, err = testClient.Ping(ctx, &emptypb.Empty{})
			if tt.expectError {
				require.Error(t, err, "Expected timeout error")
				assert.Equal(t, tt.errorCode, status.Code(err), "Expected correct error code")
			} else {
				require.NoError(t, err, "Expected success")
			}
		})
	}
}

func TestIntegration_AllFeatures(t *testing.T) {
	// Создаем временную директорию для сертификатов
	tempDir, err := os.MkdirTemp("", "grpc_test_certs")
	require.NoError(t, err, "Failed to create temp dir")
	defer os.RemoveAll(tempDir)

	// Генерируем тестовые сертификаты
	certs, err := generateTestCerts(tempDir)
	require.NoError(t, err, "Failed to generate test certificates")

	// Настройка логирования
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	// Запуск тестового сервера с TLS
	server, err := NewTestServer(true, certs.CertFile, certs.KeyFile, &logger)
	require.NoError(t, err, "Failed to create TLS test server")
	defer server.Stop()

	err = server.Start()
	require.NoError(t, err, "Failed to start TLS test server")

	// Конфигурация клиента со всеми функциями
	cfg := &client.Config{
		Endpoints:     server.Addr(),
		Timeout:       10 * time.Second,
		EnableLogging: true,
		EnableTLS:     true,
		TLSConfig: &client.TLSConfig{
			CAFile: certs.CAFile,
		},
		EnableRetries: true,
		Retry: &client.RetryConfig{
			MaxRetries:      2,
			BackoffTime:     50 * time.Millisecond,
			BackoffFraction: 0.1,
		},
		EnableKeepAlive: true,
		KeepAlive: &client.KeepAliveConfig{
			RefreshTime:         30 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: true,
		},
	}

	// Создание клиента
	conn, err := client.NewClient(cfg, &logger)
	require.NoError(t, err, "Failed to create client")
	defer conn.Close()

	// Тест всех функций
	testClient := gen.NewTestClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Тест обычного ping
	resp, err := testClient.Ping(ctx, &emptypb.Empty{})
	require.NoError(t, err, "Ping failed")
	assert.Equal(t, "pong", resp.Message, "Expected 'pong' response")

	// Тест медленного ping
	resp, err = testClient.SlowPing(ctx, &emptypb.Empty{})
	require.NoError(t, err, "SlowPing failed")
	assert.Equal(t, "slow pong", resp.Message, "Expected 'slow pong' response")

	// Тест failing ping (должен вернуть ошибку)
	_, err = testClient.FailingPing(ctx, &emptypb.Empty{})
	require.Error(t, err, "Expected error from FailingPing")
	assert.Equal(t, codes.Internal, status.Code(err), "Expected Internal error")
}

func TestIntegration_ClientConfigValidation(t *testing.T) {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	tests := []struct {
		config      *client.Config
		name        string
		errorMsg    string
		expectError bool
	}{
		{
			name: "Valid basic config",
			config: &client.Config{
				Endpoints:     "localhost:8080",
				Timeout:       5 * time.Second,
				EnableLogging: true,
				EnableTLS:     false,
				EnableRetries: false,
			},
			expectError: false,
		},
		{
			name: "Valid TLS config",
			config: &client.Config{
				Endpoints:     "localhost:8080",
				Timeout:       5 * time.Second,
				EnableLogging: true,
				EnableTLS:     true,
				TLSConfig: &client.TLSConfig{
					CAFile: "testdata/ca.crt",
				},
				EnableRetries: false,
			},
			expectError: true, // Файл не существует
		},
		{
			name: "Valid retry config",
			config: &client.Config{
				Endpoints:     "localhost:8080",
				Timeout:       5 * time.Second,
				EnableLogging: true,
				EnableTLS:     false,
				EnableRetries: true,
				Retry: &client.RetryConfig{
					MaxRetries:      3,
					BackoffTime:     100 * time.Millisecond,
					BackoffFraction: 0.1,
				},
			},
			expectError: false,
		},
		{
			name: "Valid keep-alive config",
			config: &client.Config{
				Endpoints:       "localhost:8080",
				Timeout:         5 * time.Second,
				EnableLogging:   true,
				EnableTLS:       false,
				EnableRetries:   false,
				EnableKeepAlive: true,
				KeepAlive: &client.KeepAliveConfig{
					RefreshTime:         30 * time.Second,
					Timeout:             5 * time.Second,
					PermitWithoutStream: true,
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := client.NewClient(tt.config, &logger)
			if tt.expectError {
				assert.Error(t, err, "Expected error for config: %s", tt.name)
			} else {
				assert.NoError(t, err, "Unexpected error for config: %s", tt.name)
				if conn != nil {
					conn.Close()
				}
			}
		})
	}
}

func TestIntegration_ConnectionStates(t *testing.T) {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	// Запуск тестового сервера
	server, err := NewTestServer(false, "", "", &logger)
	require.NoError(t, err, "Failed to create test server")
	defer server.Stop()

	err = server.Start()
	require.NoError(t, err, "Failed to start test server")

	cfg := &client.Config{
		Endpoints:     server.Addr(),
		Timeout:       5 * time.Second,
		EnableLogging: true,
		EnableTLS:     false,
		EnableRetries: false,
	}

	// Создание клиента
	conn, err := client.NewClient(cfg, &logger)
	require.NoError(t, err, "Failed to create client")
	defer conn.Close()

	// Проверяем состояние соединения
	state := conn.GetState()
	assert.NotNil(t, state, "Connection state should not be nil")

	// Тест после закрытия соединения
	conn.Close()

	// Попытка использовать закрытое соединение должна вернуть ошибку
	testClient := gen.NewTestClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err = testClient.Ping(ctx, &emptypb.Empty{})
	assert.Error(t, err, "Expected error when using closed connection")
}

// TestIntegration_RetryHonoursMaxRetries проверяет, что число попыток берётся из конфига
// (MaxRetries повторов + первая попытка), а логирование не теряется при включённых ретраях.
func TestIntegration_RetryHonoursMaxRetries(t *testing.T) {
	var logs bytes.Buffer

	logger := zerolog.New(&logs)

	server, err := NewTestServer(false, "", "", &logger)
	require.NoError(t, err)
	defer server.Stop()

	require.NoError(t, server.Start())
	server.Service().SetShouldFail(true)

	cfg := &client.Config{
		Endpoints:     server.Addr(),
		Timeout:       5 * time.Second,
		EnableLogging: true,
		EnableRetries: true,
		Retry: &client.RetryConfig{
			MaxRetries:  2,
			BackoffTime: 10 * time.Millisecond,
		},
	}

	conn, err := client.NewClient(cfg, &logger)
	require.NoError(t, err)
	defer conn.Close()

	_, err = gen.NewTestClient(conn).Ping(context.Background(), &emptypb.Empty{})
	require.Equal(t, codes.Internal, status.Code(err))

	assert.Equal(t, int64(3), server.Service().Calls(), "ожидается 1 попытка + 2 повтора")
	assert.Contains(t, logs.String(), "finished call", "логирующий интерсептор должен работать вместе с ретраями")
	assert.Contains(t, logs.String(), "gRPC retry attempt")
}
