package httpclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Request struct {
	ctx         context.Context
	client      *client
	method      string
	path        string
	headers     http.Header
	queryParams map[string]interface{}
	body        interface{}
	requestBody []byte
}

func (r *Request) SetQueryParam(key string, value interface{}) *Request {
	if r.queryParams == nil {
		r.queryParams = make(map[string]interface{})
	}
	r.queryParams[key] = value
	return r
}

func (r *Request) SetHeader(key, value string) *Request {
	r.headers.Set(key, value)
	return r
}

func (r *Request) SetBody(body interface{}) *Request {
	r.body = body
	return r
}

func (r *Request) Execute(target interface{}) (resp *http.Response, lastErr error) {
	fullURL, err := r.buildURL()
	if err != nil {
		return nil, fmt.Errorf("r.buildURL: %w", err)
	}

	startTime := time.Now()

	req, err := r.createHTTPRequest(fullURL)
	if err != nil {
		return nil, fmt.Errorf("r.createHTTPRequest: %w", err)
	}

	maxRetries := r.getMaxRetries()

	for attempt := uint(0); attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := r.calculateBackoff(attempt)
			select {
			case <-r.ctx.Done():
				return nil, r.ctx.Err()
			case <-time.After(backoff):
			}
			r.client.log.Log(r.ctx, Warning, "Retrying request",
				"attempt", attempt,
				"url", fullURL,
				"backoff", backoff,
			)
		}

		resp, lastErr = r.client.client.Do(req)

		shouldRetry := false
		if lastErr == nil && resp != nil {
			shouldRetry = r.isRetryableStatus(resp.StatusCode)
		} else if lastErr != nil {
			shouldRetry = !errors.Is(lastErr, context.Canceled) && !errors.Is(lastErr, context.DeadlineExceeded)
		}

		if shouldRetry && attempt < maxRetries {
			if resp != nil {
				closeErr := resp.Body.Close()
				if closeErr != nil {
					r.client.log.Log(r.ctx, Warning, "resp.Body.Close", "err", closeErr)
				}
			}
			continue
		}

		break
	}

	duration := time.Since(startTime)

	if resp == nil || lastErr != nil {
		r.logRequestResponse(req, nil, nil, duration, lastErr)
		if lastErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrRequestFailed, lastErr)
		}
		return nil, fmt.Errorf("%w: %w", ErrRequestFailed, ErrNoResponseReceived)
	}

	responseBody, readErr := io.ReadAll(resp.Body)
	defer resp.Body.Close()
	if readErr != nil {
		r.logRequestResponse(req, resp, nil, duration, readErr)
		return nil, fmt.Errorf("io.ReadAll: %w", readErr)
	}

	if target != nil && len(responseBody) > 0 {
		if err := r.deserializeBody(responseBody, resp.Header.Get("Content-Type"), target); err != nil {
			if resp.StatusCode < 400 {
				r.logRequestResponse(req, resp, responseBody, duration, err)
				return resp, fmt.Errorf("%w: %w", ErrFailedToDeserialize, err)
			}
		}
	}

	var httpErr error
	if resp.StatusCode >= 400 {
		httpErr = &HTTPError{
			StatusCode:   resp.StatusCode,
			URL:          fullURL,
			ResponseBody: responseBody,
			err:          lastErr,
		}
	}

	r.logRequestResponse(req, resp, responseBody, duration, httpErr)

	if resp.StatusCode >= 400 {
		return resp, httpErr
	}

	return resp, nil
}

func (r *Request) buildURL() (string, error) {
	baseURL := r.client.config.BaseURL
	path := r.path

	if parsedPath, err := url.Parse(path); err != nil || parsedPath.Scheme == "" || parsedPath.Host == "" {
		path = baseURL + path
	}
	u, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}

	if len(r.queryParams) > 0 {
		q := u.Query()
		for key, value := range r.queryParams {
			var strValue string
			switch v := value.(type) {
			case string:
				strValue = v
			case []string:
				strValue = strings.Join(v, ",")
			default:
				strValue = fmt.Sprintf("%v", v)
			}
			q.Set(key, strValue)
		}
		u.RawQuery = q.Encode()
	}

	return u.String(), nil
}

func (r *Request) createHTTPRequest(fullURL string) (*http.Request, error) {
	var bodyBytes []byte
	var bodyReader io.Reader

	if r.body != nil {
		var err error
		bodyBytes, err = r.serializeBody()
		if err != nil {
			return nil, fmt.Errorf("r.serializeBody: %w", err)
		}
		r.requestBody = bodyBytes //TODO посмотреть как можно без этого
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(r.ctx, r.method, fullURL, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header = r.headers

	return req, nil
}

func (r *Request) isRetryableStatus(statusCode int) bool {
	for _, code := range DefaultRetryableStatusCodes() {
		if statusCode == code {
			return true
		}
	}
	return false
}
