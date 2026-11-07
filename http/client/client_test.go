package httpclient

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestServer struct {
	server *httptest.Server
}

func NewTestServer() *TestServer {
	ts := &TestServer{}
	ts.server = httptest.NewServer(http.HandlerFunc(ts.handleRequest))
	return ts
}

func (ts *TestServer) URL() string {
	return ts.server.URL
}

func (ts *TestServer) Close() {
	ts.server.Close()
}

var globalTestServer *TestServer

func TestMain(m *testing.M) {
	globalTestServer = NewTestServer()
	defer globalTestServer.Close()

	code := m.Run()

	os.Exit(code)
}

func (ts *TestServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	queryParams := r.URL.Query()

	headers := make(map[string]string)
	for key, values := range r.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}

	path := r.URL.Path

	switch path {
	case "/test/get/success":
		ts.handleSuccess(w, r, body, queryParams, headers)

	case "/test/get/with-params":
		ts.handleWithParams(w, r, body, queryParams, headers)

	case "/test/get/with-headers":
		ts.handleWithHeaders(w, r, body, queryParams, headers)

	case "/test/post/with-body":
		ts.handlePostWithBody(w, r, body, queryParams, headers)

	case "/test/put/with-body":
		ts.handlePutWithBody(w, r, body, queryParams, headers)

	case "/test/patch/with-body":
		ts.handlePatchWithBody(w, r, body, queryParams, headers)

	case "/test/delete/success":
		ts.handleDeleteSuccess(w, r, body, queryParams, headers)

	case "/test/error/400":
		ts.handleError(w, 400, "Bad Request", body)

	case "/test/error/404":
		ts.handleError(w, 404, "Not Found", body)

	case "/test/error/429":
		ts.handleError(w, 429, "Too Many Requests", body)

	case "/test/error/500":
		ts.handleError(w, 500, "Internal Server Error", body)

	case "/test/error/502":
		ts.handleError(w, 502, "Bad Gateway", body)

	case "/test/error/503":
		ts.handleError(w, 503, "Service Unavailable", body)

	case "/test/error/504":
		ts.handleError(w, 504, "Gateway Timeout", body)

	case "/test/retry/success-after-retry":
		ts.handleRetrySuccess(w, r, body, queryParams, headers)

	case "/test/response/json":
		ts.handleJSONResponse(w, r, body, queryParams, headers)

	case "/test/response/xml":
		ts.handleXMLResponse(w, r, body, queryParams, headers)

	case "/test/response/empty":
		ts.handleEmptyResponse(w, r, body, queryParams, headers)

	case "/test/response/invalid-json":
		ts.handleInvalidJSON(w, r, body, queryParams, headers)

	case "/test/timeout":
		ts.handleTimeout(w, r, body, queryParams, headers)

	case "/test/context-cancel":
		ts.handleContextCancel(w, r, body, queryParams, headers)

	default:
		ts.handleError(w, 404, fmt.Sprintf("Path not found: %s", path), body)
	}
}

func (ts *TestServer) handleSuccess(w http.ResponseWriter, r *http.Request, body []byte, queryParams map[string][]string, headers map[string]string) {
	response := map[string]interface{}{
		"status": "success",
		"method": r.Method,
		"path":   r.URL.Path,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ts *TestServer) handleWithParams(w http.ResponseWriter, r *http.Request, body []byte, queryParams map[string][]string, headers map[string]string) {
	response := map[string]interface{}{
		"status":      "success",
		"queryParams": queryParams,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ts *TestServer) handleWithHeaders(w http.ResponseWriter, r *http.Request, body []byte, queryParams map[string][]string, headers map[string]string) {
	response := map[string]interface{}{
		"status":  "success",
		"headers": headers,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ts *TestServer) handlePostWithBody(w http.ResponseWriter, r *http.Request, body []byte, queryParams map[string][]string, headers map[string]string) {
	var requestBody map[string]interface{}
	if len(body) > 0 {
		json.Unmarshal(body, &requestBody)
	}

	response := map[string]interface{}{
		"status":      "created",
		"method":      r.Method,
		"requestBody": requestBody,
		"contentType": headers["Content-Type"],
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

func (ts *TestServer) handlePutWithBody(w http.ResponseWriter, r *http.Request, body []byte, queryParams map[string][]string, headers map[string]string) {
	var requestBody map[string]interface{}
	if len(body) > 0 {
		json.Unmarshal(body, &requestBody)
	}

	response := map[string]interface{}{
		"status":      "updated",
		"method":      r.Method,
		"requestBody": requestBody,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ts *TestServer) handlePatchWithBody(w http.ResponseWriter, r *http.Request, body []byte, queryParams map[string][]string, headers map[string]string) {
	var requestBody map[string]interface{}
	if len(body) > 0 {
		json.Unmarshal(body, &requestBody)
	}

	response := map[string]interface{}{
		"status":      "patched",
		"method":      r.Method,
		"requestBody": requestBody,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ts *TestServer) handleDeleteSuccess(w http.ResponseWriter, r *http.Request, body []byte, queryParams map[string][]string, headers map[string]string) {
	response := map[string]interface{}{
		"status": "deleted",
		"method": r.Method,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ts *TestServer) handleError(w http.ResponseWriter, statusCode int, message string, body []byte) {
	errorResponse := map[string]interface{}{
		"error":   message,
		"code":    statusCode,
		"request": string(body),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(errorResponse)
}

func (ts *TestServer) handleRetrySuccess(w http.ResponseWriter, r *http.Request, body []byte, queryParams map[string][]string, headers map[string]string) {
	attempt := headers["X-Retry-Attempt"]
	if attempt == "" || attempt == "0" {
		ts.handleError(w, 500, "Internal Server Error - retry needed", body)
		return
	}

	// Последующие попытки - успех
	response := map[string]interface{}{
		"status":  "success",
		"attempt": attempt,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ts *TestServer) handleJSONResponse(w http.ResponseWriter, r *http.Request, body []byte, queryParams map[string][]string, headers map[string]string) {
	response := map[string]interface{}{
		"id":    1,
		"name":  "test",
		"value": 42.5,
		"tags":  []string{"tag1", "tag2"},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ts *TestServer) handleXMLResponse(w http.ResponseWriter, r *http.Request, body []byte, queryParams map[string][]string, headers map[string]string) {
	type XMLResponse struct {
		XMLName xml.Name `xml:"response"`
		ID      int      `xml:"id"`
		Name    string   `xml:"name"`
		Value   float64  `xml:"value"`
	}

	response := XMLResponse{
		ID:    1,
		Name:  "test",
		Value: 42.5,
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(response)
}

func (ts *TestServer) handleEmptyResponse(w http.ResponseWriter, r *http.Request, body []byte, queryParams map[string][]string, headers map[string]string) {
	w.WriteHeader(http.StatusNoContent)
}

func (ts *TestServer) handleInvalidJSON(w http.ResponseWriter, r *http.Request, body []byte, queryParams map[string][]string, headers map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"invalid": json}`))
}

func (ts *TestServer) handleTimeout(w http.ResponseWriter, r *http.Request, body []byte, queryParams map[string][]string, headers map[string]string) {
	time.Sleep(2 * time.Second)
	response := map[string]interface{}{
		"status": "timeout",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (ts *TestServer) handleContextCancel(w http.ResponseWriter, r *http.Request, body []byte, queryParams map[string][]string, headers map[string]string) {
	// Долгая задержка для возможности отмены контекста
	time.Sleep(5 * time.Second)
	response := map[string]interface{}{
		"status": "should not reach here",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func TestClient_HTTPMethods(t *testing.T) {
	tests := []struct {
		name                string
		method              string
		path                string
		requestBody         map[string]interface{}
		expectedStatus      int
		expectedStatusField string
	}{
		{
			name:                "GET basic",
			method:              "GET",
			path:                "/test/get/success",
			requestBody:         nil,
			expectedStatus:      http.StatusOK,
			expectedStatusField: "success",
		},
		{
			name:                "POST with body",
			method:              "POST",
			path:                "/test/post/with-body",
			requestBody:         map[string]interface{}{"name": "test", "value": 42},
			expectedStatus:      http.StatusCreated,
			expectedStatusField: "created",
		},
		{
			name:                "PUT with body",
			method:              "PUT",
			path:                "/test/put/with-body",
			requestBody:         map[string]interface{}{"id": 1, "name": "updated"},
			expectedStatus:      http.StatusOK,
			expectedStatusField: "updated",
		},
		{
			name:                "PATCH with body",
			method:              "PATCH",
			path:                "/test/patch/with-body",
			requestBody:         map[string]interface{}{"name": "patched"},
			expectedStatus:      http.StatusOK,
			expectedStatusField: "patched",
		},
		{
			name:                "DELETE",
			method:              "DELETE",
			path:                "/test/delete/success",
			requestBody:         nil,
			expectedStatus:      http.StatusOK,
			expectedStatusField: "deleted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := Config{
				BaseURL: globalTestServer.URL(),
			}
			client, err := NewClient(cfg)
			require.NoError(t, err)

			ctx := context.Background()
			var result map[string]interface{}

			req := client.R(ctx, tt.method, tt.path)

			if tt.requestBody != nil {
				req.SetHeader("Content-Type", "application/json").
					SetBody(tt.requestBody)
			}

			resp, err := req.Execute(&result)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, resp.StatusCode)
			assert.Equal(t, tt.expectedStatusField, result["status"])

			if method, ok := result["method"].(string); ok {
				assert.Equal(t, tt.method, method)
			}

			if tt.method == "POST" && tt.requestBody != nil {
				receivedBody, ok := result["requestBody"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, tt.requestBody["name"], receivedBody["name"])
				assert.Equal(t, float64(tt.requestBody["value"].(int)), receivedBody["value"])
			}
		})
	}
}

func TestClient_Get_WithParams(t *testing.T) {
	t.Parallel()

	cfg := Config{
		BaseURL: globalTestServer.URL(),
	}
	client, err := NewClient(cfg)
	require.NoError(t, err)

	ctx := context.Background()
	var result map[string]interface{}
	resp, err := client.Get(ctx, "/test/get/with-params").
		SetQueryParam("key1", "value1").
		SetQueryParam("key2", "value2").
		SetQueryParam("number", 42).
		SetQueryParam("tags", []string{"tag1", "tag2"}).
		Execute(&result)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	queryParams, ok := result["queryParams"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "value1", queryParams["key1"].([]interface{})[0])
	assert.Equal(t, "value2", queryParams["key2"].([]interface{})[0])
}

func TestClient_Get_WithHeaders(t *testing.T) {
	t.Parallel()

	cfg := Config{
		BaseURL: globalTestServer.URL(),
	}
	client, err := NewClient(cfg,
		WithHeader("X-Global-Header", "global-value"),
	)
	require.NoError(t, err)

	ctx := context.Background()
	var result map[string]interface{}
	resp, err := client.Get(ctx, "/test/get/with-headers").
		SetHeader("X-Custom-Header", "custom-value").
		SetHeader("Authorization", "Bearer token123").
		Execute(&result)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	headers, ok := result["headers"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "custom-value", headers["X-Custom-Header"])
	assert.Equal(t, "Bearer token123", headers["Authorization"])
	assert.Equal(t, "global-value", headers["X-Global-Header"])
}

func TestClient_Errors(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		expectedStatus int
	}{
		{
			name:           "400 Bad Request",
			path:           "/test/error/400",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "404 Not Found",
			path:           "/test/error/404",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "500 Internal Server Error",
			path:           "/test/error/500",
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := Config{
				BaseURL: globalTestServer.URL(),
			}
			client, err := NewClient(cfg)
			require.NoError(t, err)

			ctx := context.Background()
			var result map[string]interface{}
			resp, err := client.Get(ctx, tt.path).Execute(&result)

			require.Error(t, err)
			assert.NotNil(t, resp)
			assert.Equal(t, tt.expectedStatus, resp.StatusCode)

			httpErr, ok := AsHTTPError(err)
			require.True(t, ok, "Ошибка должна быть типа HTTPError для статуса %d", tt.expectedStatus)
			assert.Equal(t, tt.expectedStatus, httpErr.StatusCode)
			assert.True(t, IsHTTPError(err))
		})
	}
}

func TestClient_ResponseFormats(t *testing.T) {
	type JSONResponse struct {
		ID    int      `json:"id"`
		Name  string   `json:"name"`
		Value float64  `json:"value"`
		Tags  []string `json:"tags"`
	}

	type XMLResponse struct {
		XMLName xml.Name `xml:"response"`
		ID      int      `xml:"id"`
		Name    string   `xml:"name"`
		Value   float64  `xml:"value"`
	}

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectError    bool
		errorContains  string
		jsonResponse   *JSONResponse
		xmlResponse    *XMLResponse
		emptyResponse  bool
	}{
		{
			name:           "JSON response",
			path:           "/test/response/json",
			expectedStatus: http.StatusOK,
			expectError:    false,
			jsonResponse:   &JSONResponse{},
		},
		{
			name:           "XML response",
			path:           "/test/response/xml",
			expectedStatus: http.StatusOK,
			expectError:    false,
			xmlResponse:    &XMLResponse{},
		},
		{
			name:           "Empty response",
			path:           "/test/response/empty",
			expectedStatus: http.StatusNoContent,
			expectError:    false,
			emptyResponse:  true,
		},
		{
			name:           "Invalid JSON",
			path:           "/test/response/invalid-json",
			expectedStatus: http.StatusOK,
			expectError:    true,
			errorContains:  "failed to deserialize",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := Config{
				BaseURL: globalTestServer.URL(),
			}
			client, err := NewClient(cfg)
			require.NoError(t, err)

			ctx := context.Background()
			var resp *http.Response

			if tt.jsonResponse != nil {
				resp, err = client.Get(ctx, tt.path).Execute(tt.jsonResponse)
				if !tt.expectError {
					require.NoError(t, err)
					assert.Equal(t, 1, tt.jsonResponse.ID)
					assert.Equal(t, "test", tt.jsonResponse.Name)
					assert.Equal(t, 42.5, tt.jsonResponse.Value)
					assert.Equal(t, []string{"tag1", "tag2"}, tt.jsonResponse.Tags)
				}
			} else if tt.xmlResponse != nil {
				resp, err = client.Get(ctx, tt.path).Execute(tt.xmlResponse)
				if !tt.expectError {
					require.NoError(t, err)
					assert.Equal(t, 1, tt.xmlResponse.ID)
					assert.Equal(t, "test", tt.xmlResponse.Name)
					assert.Equal(t, 42.5, tt.xmlResponse.Value)
				}
			} else if tt.emptyResponse {
				var emptyResult map[string]interface{}
				resp, err = client.Get(ctx, tt.path).Execute(&emptyResult)
				if !tt.expectError {
					require.NoError(t, err)
				}
			} else {
				var invalidResult map[string]interface{}
				resp, err = client.Get(ctx, tt.path).Execute(&invalidResult)
			}

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.True(t, strings.Contains(err.Error(), tt.errorContains))
				}
				if resp != nil {
					assert.Equal(t, tt.expectedStatus, resp.StatusCode)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedStatus, resp.StatusCode)
			}
		})
	}
}

func TestClient_Context_Cancel(t *testing.T) {
	t.Parallel()

	cfg := Config{
		BaseURL: globalTestServer.URL(),
	}
	client, err := NewClient(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	cancel()

	var result map[string]interface{}
	resp, err := client.Get(ctx, "/test/context-cancel").Execute(&result)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestClient_Context_Timeout(t *testing.T) {
	t.Parallel()

	cfg := Config{
		BaseURL: globalTestServer.URL(),
	}
	client, err := NewClient(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var result map[string]interface{}
	_, err = client.Get(ctx, "/test/timeout").Execute(&result)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timeout"))
}
