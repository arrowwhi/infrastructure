package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"

	"github.com/quic-go/quic-go/http3"

	"github.com/arrowwhi/infrastructure/observability/logger"
)

type serverStrategy interface {
	ListenAndServe() error
	Shutdown(context.Context) error
	SetHandler(http.Handler)
}

// http3Strategy - стратегия для HTTP/3 сервера
type http3Strategy struct {
	server *http3.Server
}

func (h *http3Strategy) ListenAndServe() error {
	return h.server.ListenAndServe()
}

func (h *http3Strategy) Shutdown(_ context.Context) error {
	return h.server.Close()
}

func (h *http3Strategy) SetHandler(handler http.Handler) {
	h.server.Handler = handler
}

// httpStrategy - стратегия для обычного HTTP сервера
type httpStrategy struct {
	server     *http.Server
	tlsEnabled bool
}

func (h *httpStrategy) ListenAndServe() error {
	if h.tlsEnabled {
		return h.server.ListenAndServeTLS("", "")
	}
	return h.server.ListenAndServe()
}

func (h *httpStrategy) Shutdown(ctx context.Context) error {
	return h.server.Shutdown(ctx)
}

func (h *httpStrategy) SetHandler(handler http.Handler) {
	h.server.Handler = handler
}

func choiceStrategy(cfg Config) (serverStrategy, error) {
	// Если HTTP/3 включен
	if cfg.HTTP3Enabled {
		http3Server, err := initHTTP3Server(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize HTTP/3 server: %w", err)
		}
		return &http3Strategy{server: http3Server}, nil
	}

	// Создаем обычный HTTP сервер
	httpServer := &http.Server{
		Addr:              cfg.Addr(),
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	tlsEnabled := false
	if cfg.TLS.Enabled {
		tlsConfig, err := buildTLSConfig(cfg.TLS)
		if err != nil {
			return nil, fmt.Errorf("failed to build TLS config for HTTP server: %w", err)
		}
		httpServer.TLSConfig = tlsConfig
		tlsEnabled = true
	}

	return &httpStrategy{
		server:     httpServer,
		tlsEnabled: tlsEnabled,
	}, nil
}

func initHTTP3Server(config Config) (*http3.Server, error) {
	if !config.TLS.Enabled {
		return nil, fmt.Errorf("HTTP/3 enabled but TLS is not configured")
	}

	// Проверяем наличие сертификата и ключа (либо файлы, либо байты)
	hasCertData := len(config.TLS.CertData) > 0 && len(config.TLS.KeyData) > 0
	hasCertFiles := config.TLS.CertFile != "" && config.TLS.KeyFile != ""

	if !hasCertData && !hasCertFiles {
		return nil, fmt.Errorf("HTTP/3 enabled but TLS certificate or key not provided")
	}

	tlsConfig, err := buildTLSConfig(config.TLS)
	if err != nil {
		return nil, fmt.Errorf("failed to build TLS config for HTTP/3 server: %w", err)
	}

	// HTTP/3 требует TLS 1.3
	tlsConfig.MinVersion = tls.VersionTLS13

	return &http3.Server{
		Addr:        config.Addr(),
		IdleTimeout: config.IdleTimeout,
		TLSConfig:   tlsConfig,
	}, nil
}

func decodeBase64(data []byte) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}
	return decoded, nil
}

func buildTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	var cert tls.Certificate
	var err error

	// Проверяем наличие байтовых данных (приоритет над файлами)
	if len(cfg.CertData) > 0 && len(cfg.KeyData) > 0 {
		certData := cfg.CertData
		keyData := cfg.KeyData

		if cfg.Base64Encoded {
			certData, err = decodeBase64(cfg.CertData)
			if err != nil {
				return nil, fmt.Errorf("decodeBase64: %w", err)
			}
			keyData, err = decodeBase64(cfg.KeyData)
			if err != nil {
				return nil, fmt.Errorf("decodeBase64: %w", err)
			}
		}

		cert, err = tls.X509KeyPair(certData, keyData)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS certificate from bytes: %w", err)
		}
	} else if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err = tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS certificate from files: %w", err)
		}
	} else {
		return nil, fmt.Errorf("TLS certificate or key not provided (neither files nor bytes)")
	}

	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}

	if cfg.MTLSEnabled {
		// Получаем CA сертификат из байтов или файла
		caCert, err := getCAContent(cfg)
		if err != nil {
			return nil, err
		}

		err = configureMTLS(tlsConfig, caCert)
		if err != nil {
			return nil, err
		}
	}

	return tlsConfig, nil
}

func getCAContent(cfg TLSConfig) ([]byte, error) {
	if len(cfg.CAData) > 0 {
		caData := cfg.CAData
		if cfg.Base64Encoded {
			decoded, err := decodeBase64(cfg.CAData)
			if err != nil {
				return nil, fmt.Errorf("failed to decode CA data: %w", err)
			}
			caData = decoded
		}
		return caData, nil
	}

	if cfg.CAFile != "" {
		caCert, err := os.ReadFile(cfg.CAFile) //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate from file: %w", err)
		}
		return caCert, nil
	}

	return nil, fmt.Errorf("mTLS enabled but CA certificate not provided (neither file nor bytes)")
}

func configureMTLS(tlsConfig *tls.Config, caCert []byte) error {
	clientCAPool := x509.NewCertPool()
	if !clientCAPool.AppendCertsFromPEM(caCert) {
		return fmt.Errorf("failed to parse client CA certificate")
	}

	tlsConfig.ClientAuth = tls.RequireAnyClientCert
	tlsConfig.ClientCAs = clientCAPool

	logger.Info(context.Background()).Msg("Mutual TLS enabled - client certificates required")

	return nil
}
