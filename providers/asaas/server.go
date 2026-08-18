// Package asaas is the HTTP facade mirroring the Asaas API (v3): routes,
// JSON field names, event names and the webhook signature scheme all live
// here, nowhere else in localpsp. It translates requests into calls
// against engine and dispatch, and translates their results back into
// Asaas's real response shapes.
package asaas

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/danellalc/localpsp/dispatch"
	"github.com/danellalc/localpsp/engine"
)

// Server is the Asaas HTTP facade. It holds no state of its own beyond a
// small webhook subscription registry; entity truth lives in the Engine
// and delivery lives in the Dispatcher, both supplied by the caller.
type Server struct {
	engine           *engine.Engine
	dispatch         *dispatch.Dispatcher
	basePath         string
	publicURL        string
	mux              *http.ServeMux
	webhooks         *webhookRegistry
	customerProfiles *customerProfileStore
	paymentMeta      *paymentMetaStore
	lastEvents       *lastEventStore
}

// NewServer builds a Server that mounts every Asaas v3 route under
// basePath (for example "/asaas/v3"). eng and disp must already be
// constructed and are used as-is; NewServer does not take ownership of
// their lifecycle, the caller still closes them. publicURL is the
// scheme+host(+port) this Server is actually reachable at (for example
// "http://localhost:8420"), used to build a real, resolvable invoiceUrl
// on a payment; pass "" if that's not needed (a real invoiceUrl won't
// resolve, but nothing else in this package depends on it).
func NewServer(eng *engine.Engine, disp *dispatch.Dispatcher, basePath, publicURL string) *Server {
	s := &Server{
		engine:           eng,
		dispatch:         disp,
		basePath:         normalizeBasePath(basePath),
		publicURL:        strings.TrimSuffix(publicURL, "/"),
		webhooks:         newWebhookRegistry(),
		customerProfiles: newCustomerProfileStore(),
		paymentMeta:      newPaymentMetaStore(),
		lastEvents:       newLastEventStore(),
	}
	s.mux = http.NewServeMux()
	s.routes()
	return s
}

// maxRequestBodyBytes bounds every request body this facade reads. Every
// payload it accepts (a customer, a payment, a webhook config) is tiny;
// this only exists to stop an unbounded json.Decode from buffering an
// arbitrarily large body into memory.
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// ServeHTTP makes Server an http.Handler, ready to mount into
// http.ListenAndServe or wrap in an httptest.Server. It caps request body
// size and makes sure even an unmatched route or wrong method comes back
// as this facade's own {"errors": [...]} JSON shape, not net/http's
// default plain text 404/405.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	s.mux.ServeHTTP(&jsonErrorInterceptor{ResponseWriter: w}, r)
}

// jsonErrorInterceptor rewrites net/http's default 404/405 responses
// (plain text) into this facade's own JSON error shape, so every non-2xx
// response from this package looks the same to a client.
type jsonErrorInterceptor struct {
	http.ResponseWriter
	intercepting bool
}

func (w *jsonErrorInterceptor) WriteHeader(status int) {
	unmatched := status == http.StatusNotFound || status == http.StatusMethodNotAllowed
	// A handler that already called writeJSON/writeError set Content-Type
	// to exactly "application/json" itself before reaching here, that's
	// the signal to leave it alone, not the status code alone: this
	// facade's own handlers legitimately return 404 too (customer_not_found
	// and friends). net/http's own default 404/405 responses aren't a
	// blank Content-Type, net/http's internal http.Error sets
	// "text/plain; charset=utf-8" before calling WriteHeader, so checking
	// for merely non-empty here would (and once did) let that default
	// response straight through unrewritten.
	alreadyHandled := w.ResponseWriter.Header().Get("Content-Type") == "application/json"
	if !unmatched || alreadyHandled {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.intercepting = true
	code, description := "not_found", "resource not found"
	if status == http.StatusMethodNotAllowed {
		code, description = "method_not_allowed", "method not allowed on this resource"
	}
	w.ResponseWriter.Header().Set("Content-Type", "application/json")
	w.ResponseWriter.WriteHeader(status)
	_ = json.NewEncoder(w.ResponseWriter).Encode(errorResponse{Errors: []apiError{{Code: code, Description: description}}})
}

func (w *jsonErrorInterceptor) Write(b []byte) (int, error) {
	if w.intercepting {
		// The JSON body was already written by WriteHeader; swallow
		// net/http's own plain text body instead of appending to ours.
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

func normalizeBasePath(basePath string) string {
	if basePath == "" {
		return ""
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	return strings.TrimSuffix(basePath, "/")
}

func (s *Server) routes() {
	p := s.basePath
	s.mux.HandleFunc("POST "+p+"/customers", s.handleCreateCustomer)
	s.mux.HandleFunc("GET "+p+"/customers/{id}", s.handleGetCustomer)
	s.mux.HandleFunc("POST "+p+"/payments", s.handleCreatePayment)
	s.mux.HandleFunc("GET "+p+"/payments/{id}", s.handleGetPayment)
	s.mux.HandleFunc("GET "+p+"/invoices/{id}", s.handleGetPayment)
	s.mux.HandleFunc("POST "+p+"/sandbox/payment/{id}/confirm", s.handleConfirmPayment)
	s.mux.HandleFunc("POST "+p+"/subscriptions", s.handleCreateSubscription)
	s.mux.HandleFunc("GET "+p+"/subscriptions/{id}", s.handleGetSubscription)
	s.mux.HandleFunc("POST "+p+"/webhooks", s.handleCreateWebhook)
	s.mux.HandleFunc("PUT "+p+"/webhooks/{id}", s.handleUpdateWebhook)

	s.adminRoutes()
}
