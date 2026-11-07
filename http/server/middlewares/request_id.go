package middlewares

import (
	"context"
	"net/http"

	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/google/uuid"
)

// Key to use when setting the request ID.
type ctxKeyRequestID struct{}

var RequestIDKey = ctxKeyRequestID{}

const RequestIDHeader = "X-Request-ID"

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := r.Header.Get(RequestIDHeader)
		if requestID == "" {
			newUUID, err := uuid.NewRandom()
			if err != nil {
				logger.Error(ctx).Msg("failed to generate UUID")
			} else {
				requestID = newUUID.String()
			}
		}

		ctx = context.WithValue(ctx, RequestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
