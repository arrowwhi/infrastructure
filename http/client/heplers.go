package httpclient

import (
	"math/rand"
	"net/http"
	"time"
)

func (r *Request) logRequestResponse(req *http.Request, resp *http.Response, responseBody []byte, duration time.Duration, err error) {
	if r.client.log == nil {
		return
	}

	fields := []any{
		"method", req.Method,
		"url", req.URL.String(),
		"duration_ms", duration.Milliseconds(),
	}

	if len(r.requestBody) > 0 {
		requestBodyStr := string(r.requestBody)
		if len(requestBodyStr) > 1000 {
			requestBodyStr = requestBodyStr[:1000] + "... (truncated)"
		}
		fields = append(fields, "request_body", requestBodyStr)
	}

	if len(req.Header) > 0 {
		fields = append(fields, "request_headers", req.Header)
	}

	if resp != nil {
		fields = append(fields, "status", resp.StatusCode)

		if len(responseBody) > 0 {
			responseBodyStr := string(responseBody)
			if len(responseBodyStr) > 1000 {
				responseBodyStr = responseBodyStr[:1000] + "... (truncated)"
			}
			fields = append(fields, "response_body", responseBodyStr)
		}

		if len(resp.Header) > 0 {
			fields = append(fields, "response_headers", resp.Header)
		}
	}

	if err != nil {
		fields = append(fields, "error", err.Error())
		if httpErr, ok := err.(*HTTPError); ok {
			fields = append(fields, "http_status", httpErr.StatusCode)
		}
	}

	level := Info
	if err != nil {
		level = Error
	} else if resp != nil {
		if resp.StatusCode >= 400 {
			level = Error
		} else if resp.StatusCode >= 300 {
			level = Warning
		}
	}

	r.client.log.Log(r.ctx, level, "HTTP request", fields...)
}

func (r *Request) getMaxRetries() uint {
	if r.client.config.Retry != nil {
		return r.client.config.Retry.MaxRetries
	}
	return 0
}

func (r *Request) calculateBackoff(attempt uint) time.Duration {
	if r.client.config.Retry == nil {
		return 0
	}

	baseDelay := r.client.config.Retry.BackoffTime

	const maxExponent = 20
	exponent := attempt - 1
	if exponent > maxExponent {
		exponent = maxExponent
	}

	// Экспоненциальный рост: baseDelay * 2^exponent
	delay := baseDelay * time.Duration(1<<exponent)

	if r.client.config.Retry.BackoffFraction > 0 {
		jitter := time.Duration(float64(delay) * r.client.config.Retry.BackoffFraction * (rand.Float64()*2 - 1))
		delay += jitter
	}

	if delay > maxBackoffDelay {
		delay = maxBackoffDelay
	}

	return delay
}
