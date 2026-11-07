package httpclient

import "time"

const maxBackoffDelay = 30 * time.Second

const (
	// DefaultRetryableStatusTooManyRequests - статус 429 (Too Many Requests)
	DefaultRetryableStatusTooManyRequests = 429
	// DefaultRetryableStatusInternalServerError - статус 500 (Internal Server Error)
	DefaultRetryableStatusInternalServerError = 500
	// DefaultRetryableStatusBadGateway - статус 502 (Bad Gateway)
	DefaultRetryableStatusBadGateway = 502
	// DefaultRetryableStatusServiceUnavailable - статус 503 (Service Unavailable)
	DefaultRetryableStatusServiceUnavailable = 503
	// DefaultRetryableStatusGatewayTimeout - статус 504 (Gateway Timeout)
	DefaultRetryableStatusGatewayTimeout = 504
)

func DefaultRetryableStatusCodes() []int {
	return []int{
		DefaultRetryableStatusTooManyRequests,
		DefaultRetryableStatusInternalServerError,
		DefaultRetryableStatusBadGateway,
		DefaultRetryableStatusServiceUnavailable,
		DefaultRetryableStatusGatewayTimeout,
	}
}

type SerializerFunc func(interface{}) ([]byte, error)
type DeserializerFunc func([]byte, interface{}) error
