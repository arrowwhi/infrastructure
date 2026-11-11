package middlewares

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// captureStdout перехватывает os.Stdout, куда пишет логгер по умолчанию
// (observability/logger без Init создаёт логгер на os.Stdout при каждом вызове).
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	orig := os.Stdout
	os.Stdout = w

	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()

	func() {
		defer func() { os.Stdout = orig }()
		fn()
	}()

	_ = w.Close()
	out := <-done
	_ = r.Close()

	return string(out)
}

func accessLogRecord(t *testing.T, out string) map[string]any {
	t.Helper()

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec["message"] == "incoming HTTP request" {
			return rec
		}
	}

	t.Fatalf("access log record not found in output: %q", out)

	return nil
}

func TestLogBodies(t *testing.T) {
	const (
		reqBody  = `{"player":"Alice","amount":100}`
		respBody = `{"player":"Bob","balance":500}`
	)

	tests := []struct {
		name       string
		mw         func(http.Handler) http.Handler
		wantBodies bool
	}{
		{name: "Log logs bodies by default", mw: Log, wantBodies: true},
		{name: "NewLog with zero options logs bodies", mw: NewLog(LogOptions{}), wantBodies: true},
		{name: "NewLog with DisableBodies skips bodies", mw: NewLog(LogOptions{DisableBodies: true}), wantBodies: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotReqBody string

			h := tt.mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				gotReqBody = string(b)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(respBody))
			}))

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/sessions/42", bytes.NewBufferString(reqBody))
			req.Header.Set("User-Agent", "test-agent")

			out := captureStdout(t, func() { h.ServeHTTP(rec, req) })

			// Handler и клиент получают тела без изменений в обоих режимах.
			if gotReqBody != reqBody {
				t.Errorf("handler got request body %q, want %q", gotReqBody, reqBody)
			}
			if rec.Body.String() != respBody {
				t.Errorf("client got response body %q, want %q", rec.Body.String(), respBody)
			}

			logRec := accessLogRecord(t, out)

			_, hasReq := logRec["request_body"]
			_, hasResp := logRec["response_body"]
			if hasReq != tt.wantBodies || hasResp != tt.wantBodies {
				t.Errorf("request_body present=%v, response_body present=%v, want %v; record: %v",
					hasReq, hasResp, tt.wantBodies, logRec)
			}
			if !tt.wantBodies && (strings.Contains(out, "Alice") || strings.Contains(out, "Bob")) {
				t.Errorf("log output contains body data: %q", out)
			}

			for key, want := range map[string]any{
				"method":      http.MethodPost,
				"path":        "/sessions/42",
				"status_code": float64(http.StatusCreated),
				"user_agent":  "test-agent",
			} {
				if logRec[key] != want {
					t.Errorf("%s = %v, want %v", key, logRec[key], want)
				}
			}
			for _, key := range []string{"latency", "remote_addr", "status", "protocol", "bytes_in"} {
				if _, ok := logRec[key]; !ok {
					t.Errorf("field %q missing in record: %v", key, logRec)
				}
			}
		})
	}
}

func TestLogDisableBodiesDoesNotReadRequestBody(t *testing.T) {
	body := &trackingReader{r: strings.NewReader("payload")}

	h := NewLog(LogOptions{DisableBodies: true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != body {
			t.Errorf("request body was replaced by middleware")
		}
		if body.read {
			t.Errorf("request body was read before the handler")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Body = body

	captureStdout(t, func() { h.ServeHTTP(httptest.NewRecorder(), req) })
}

type trackingReader struct {
	r    io.Reader
	read bool
}

func (t *trackingReader) Read(p []byte) (int, error) {
	t.read = true
	return t.r.Read(p)
}

func (t *trackingReader) Close() error { return nil }
