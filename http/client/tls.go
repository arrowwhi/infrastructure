package httpclient

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
)

var ErrFailedToLoadClientCertificate = errors.New("failed to load client certificate")

// buildTLSConfig создает конфигурацию TLS на основе настроек из TLSConfig.
// Поддерживает:
//   - InsecureSkipVerify для отключения проверки сертификата сервера
//   - RootCAs для использования кастомных CA-сертификатов
//   - mTLS (mutual TLS) через ClientCert и ClientKey
//
// Возвращает nil, если TLSConfig не указан.
func buildTLSConfig(cfg *TLSConfig) (*tls.Config, error) {
	if cfg == nil {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}

	// Настройка RootCAs, если указаны
	if len(cfg.RootCAs) > 0 {
		rootCAs := x509.NewCertPool()
		if !rootCAs.AppendCertsFromPEM(cfg.RootCAs) {
			// Если не удалось добавить сертификаты, используем системные
			rootCAs = nil
		} else {
			tlsConfig.RootCAs = rootCAs
		}
	}

	// Настройка mTLS, если указаны клиентские сертификаты
	if len(cfg.ClientCert) > 0 && len(cfg.ClientKey) > 0 {
		cert, err := tls.X509KeyPair(cfg.ClientCert, cfg.ClientKey)
		if err != nil {
			return tlsConfig, ErrFailedToLoadClientCertificate
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}
