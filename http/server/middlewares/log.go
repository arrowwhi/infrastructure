package middlewares

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/rs/zerolog"
)

const (
	// maxBodySize максимальный размер тела запроса/ответа для логирования (512KB)
	maxBodySize = 1 << 19
)

type ctxKeyError struct{}

var ErrorKey = ctxKeyError{}

// SetError attaches err to the request so the Log middleware logs it as the
// cause of a 5xx response instead of deriving a message from the status code.
// It mutates *r in place (rather than just returning a new context) because
// Log's deferred logger already holds a pointer to the original *http.Request;
// reassigning a local `r = r.WithContext(ctx)` inside the handler would not be
// visible there.
func SetError(r *http.Request, err error) {
	*r = *r.WithContext(context.WithValue(r.Context(), ErrorKey, err))
}

//nolint:gocognit
func Log(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lrw := &loggingResponseWriter{
			ResponseWriter: w,
			body:           &bytes.Buffer{},
		}
		start := time.Now()

		var requestBody []byte
		if r.Body != nil { //nolint:nestif
			bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
			if err == nil {
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
				if len(bodyBytes) <= maxBodySize {
					requestBody = bodyBytes
				} else {
					requestBody = bodyBytes[:maxBodySize]
				}
			}
		}

		defer func() {
			ctx := r.Context()
			var logEvent *zerolog.Event

			statusCode := lrw.statusCode

			var err error
			switch {
			case statusCode < 400:
				logEvent = logger.Info(ctx)
			case statusCode >= 400 && statusCode < 500:
				logEvent = logger.Warn(ctx)
			default: // >= 500
				if ctxErr, ok := ctx.Value(ErrorKey).(error); ok && ctxErr != nil {
					err = ctxErr
					logEvent = logger.Error(ctx).Err(err)
				} else {
					logEvent = logger.Error(ctx)
				}
			}

			status := lrw.status
			if status == "" {
				status = http.StatusText(statusCode)
			}

			logEvent = logEvent.
				Str("remote_addr", r.RemoteAddr).
				Str("path", r.URL.Path).
				Str("protocol", r.Proto).
				Str("method", r.Method).
				Str("user_agent", r.UserAgent()).
				Str("status", status).
				Int("status_code", statusCode).
				Dur("latency", time.Since(start)).
				Int64("bytes_in", r.ContentLength)

			if requestID, ok := ctx.Value(RequestIDKey).(string); ok && requestID != "" {
				logEvent = logEvent.Str("request_id", requestID)
			}

			if len(requestBody) > 0 {
				logEvent = addBodyToLog(logEvent, "request_body", requestBody)
			}

			if lrw.body.Len() > 0 {
				responseBody := lrw.body.Bytes()
				if len(responseBody) > maxBodySize {
					responseBody = responseBody[:maxBodySize]
				}
				logEvent = addBodyToLog(logEvent, "response_body", responseBody)
			}

			logEvent.Msg("incoming HTTP request")
		}()

		next.ServeHTTP(lrw, r)
	})
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status     string
	statusCode int
	body       *bytes.Buffer
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.status = http.StatusText(code)
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	if lrw.body.Len() < maxBodySize {
		remaining := maxBodySize - lrw.body.Len()
		if len(b) > remaining {
			lrw.body.Write(b[:remaining])
		} else {
			lrw.body.Write(b)
		}
	}

	return lrw.ResponseWriter.Write(b)
}

// addBodyToLog добавляет тело запроса/ответа в лог без экранирования кавычек для валидного JSON
func addBodyToLog(logEvent *zerolog.Event, key string, body []byte) *zerolog.Event {
	if json.Valid(body) {
		return logEvent.RawJSON(key, body)
	}
	return logEvent.Str(key, string(body))
}
