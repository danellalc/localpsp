package asaas

import (
	"net/http"
	"testing"

	"github.com/danellalc/localpsp/dispatch"
)

// TestUnmatchedRouteReturnsJSONNotFound and
// TestWrongMethodReturnsJSONMethodNotAllowed guard jsonErrorInterceptor
// itself, an audit found it had no test coverage at all: every existing
// 404 test hits a handler that already calls writeError (Content-Type
// already set, so the interceptor's rewrite branch never runs). These
// hit a genuinely unmatched route and a genuinely mismatched method, the
// only way to actually exercise the WriteHeader/Write override.
func TestUnmatchedRouteReturnsJSONNotFound(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})

	resp := request(t, http.MethodGet, httpSrv.URL+testBasePath+"/this-route-does-not-exist", nil)
	if resp.status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusNotFound, resp.body)
	}
	var errResp errorResponse
	resp.decode(t, &errResp)
	if len(errResp.Errors) != 1 || errResp.Errors[0].Code != "not_found" {
		t.Errorf("errors = %+v, want a single not_found error", errResp.Errors)
	}
}

func TestWrongMethodReturnsJSONMethodNotAllowed(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	custID := createTestCustomer(t, httpSrv)

	// GET .../customers/{id} is registered, DELETE on the exact same path
	// is not: this is what actually exercises net/http.ServeMux's own
	// 405 detection, not just a 404 on some unrelated unmatched path.
	resp := request(t, http.MethodDelete, httpSrv.URL+testBasePath+"/customers/"+custID, nil)
	if resp.status != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusMethodNotAllowed, resp.body)
	}
	var errResp errorResponse
	resp.decode(t, &errResp)
	if len(errResp.Errors) != 1 || errResp.Errors[0].Code != "method_not_allowed" {
		t.Errorf("errors = %+v, want a single method_not_allowed error", errResp.Errors)
	}
}
