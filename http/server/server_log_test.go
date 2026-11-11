package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

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

func TestServerDisableBodyLogging(t *testing.T) {
	tests := []struct {
		name       string
		disable    bool
		wantBodies bool
	}{
		{name: "bodies logged by default", disable: false, wantBodies: true},
		{name: "DisableBodyLogging hides bodies", disable: true, wantBodies: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, err := New(Config{Host: "127.0.0.1", Port: "0", DisableBodyLogging: tt.disable})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			srv.Router().Post("/echo", func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(b)
			})

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString(`{"player":"Alice"}`))

			out := captureStdout(t, func() { srv.Handler().ServeHTTP(rec, req) })

			if rec.Body.String() != `{"player":"Alice"}` {
				t.Fatalf("response body = %q", rec.Body.String())
			}
			if !strings.Contains(out, `"path":"/echo"`) {
				t.Fatalf("access log record not found: %q", out)
			}

			for _, key := range []string{`"request_body"`, `"response_body"`} {
				if got := strings.Contains(out, key); got != tt.wantBodies {
					t.Errorf("%s present = %v, want %v; output: %q", key, got, tt.wantBodies, out)
				}
			}
		})
	}
}
