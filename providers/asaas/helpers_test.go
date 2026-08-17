package asaas

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danellalc/localpsp/dispatch"
	"github.com/danellalc/localpsp/engine"
)

const testBasePath = "/asaas/v3"

func newTestServer(t *testing.T, seed int64, dispatchOpts dispatch.Options) (*Server, *httptest.Server) {
	t.Helper()

	eng, err := engine.New(context.Background(), engine.Options{
		Seed:      seed,
		StartTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("engine.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := eng.Close(); err != nil {
			t.Fatalf("engine Close() error = %v", err)
		}
	})

	disp := dispatch.New(dispatchOpts)
	t.Cleanup(func() {
		if err := disp.Close(); err != nil {
			t.Fatalf("dispatch Close() error = %v", err)
		}
	})

	srv := NewServer(eng, disp, testBasePath, "")
	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)
	// publicURL isn't known until httptest.NewServer picks its (random)
	// port, so it's set here rather than passed into NewServer above.
	srv.publicURL = httpSrv.URL

	return srv, httpSrv
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %s", timeout)
	}
}

type apiResponse struct {
	status int
	body   []byte
}

func request(t *testing.T, method, url string, body any) apiResponse {
	t.Helper()

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, url, reader)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http request %s %s error = %v", method, url, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body error = %v", err)
	}
	return apiResponse{status: resp.StatusCode, body: data}
}

func (r apiResponse) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("unmarshal response body error = %v: %s", err, r.body)
	}
}

func createTestCustomer(t *testing.T, httpSrv *httptest.Server) string {
	t.Helper()
	resp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/customers", customerRequest{
		Name:    "Cliente Teste",
		CPFCNPJ: "12345678901",
	})
	var cust customerResponse
	resp.decode(t, &cust)
	return cust.ID
}

func boolPtr(b bool) *bool { return &b }

func strPtr(s string) *string { return &s }
