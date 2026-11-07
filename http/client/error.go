package httpclient

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrNoDeserializerFound = errors.New("no deserializer found for content type")
	ErrRequestFailed       = errors.New("request failed")
	ErrNoResponseReceived  = errors.New("no response received")
	ErrFailedToDeserialize = errors.New("failed to deserialize response")
	ErrInvalidURL          = errors.New("invalid URL")
)

type HTTPError struct {
	StatusCode   int
	URL          string
	ResponseBody []byte
	err          error
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP request failed: URL=%s, Status=%d, Error=%v, Body=%s",
		e.URL, e.StatusCode, e.err, string(e.ResponseBody))
}

func (e *HTTPError) Unwrap() error {
	return e.err
}

func CheckResponseForError(resp *http.Response, body []byte, reqURL string) error {
	if resp.StatusCode >= 400 {
		return &HTTPError{StatusCode: resp.StatusCode, URL: reqURL, ResponseBody: body, err: fmt.Errorf("HTTP status %d", resp.StatusCode)}
	}
	return nil
}

// IsHTTPError проверяет, является ли ошибка HTTPError
func IsHTTPError(err error) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr)
}

// AsHTTPError извлекает *HTTPError из ошибки, если она является HTTPError
func AsHTTPError(err error) (*HTTPError, bool) {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr, true
	}
	return nil, false
}
