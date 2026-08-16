package dispatch

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestDispatcher(t *testing.T, opts Options) *Dispatcher {
	t.Helper()
	d := New(opts)
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return d
}

// waitFor polls cond until it's true or timeout elapses, failing the
// test otherwise. Delivery happens on a background goroutine, so tests
// need to wait for it instead of asserting immediately after Enqueue.
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

func TestDeliveryPolicy(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		wantSuccess bool
	}{
		{"200 ok", http.StatusOK, true},
		{"201 created", http.StatusCreated, true},
		{"204 no content", http.StatusNoContent, true},
		{"299 edge of success range", 299, true},
		{"300 redirect", http.StatusMultipleChoices, false},
		{"404 not found", http.StatusNotFound, false},
		{"500 server error", http.StatusInternalServerError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(server.Close)

			d := newTestDispatcher(t, Options{RetryInterval: 5 * time.Millisecond})
			if err := d.RegisterEndpoint("primary", EndpointConfig{URL: server.URL}); err != nil {
				t.Fatalf("RegisterEndpoint() error = %v", err)
			}
			if err := d.Enqueue("primary", Event{ID: "evt_1", Body: []byte(`{}`)}); err != nil {
				t.Fatalf("Enqueue() error = %v", err)
			}

			waitFor(t, 2*time.Second, func() bool {
				return len(d.DeliveryLog()) >= 1
			})

			log := d.DeliveryLog()
			got := log[0]
			if got.Success != tt.wantSuccess {
				t.Errorf("Success = %v, want %v (status %d)", got.Success, tt.wantSuccess, tt.status)
			}
			if got.Status != tt.status {
				t.Errorf("Status = %d, want %d", got.Status, tt.status)
			}
			if got.EventID != "evt_1" {
				t.Errorf("EventID = %q, want %q", got.EventID, "evt_1")
			}
			if got.Endpoint != "primary" {
				t.Errorf("Endpoint = %q, want %q", got.Endpoint, "primary")
			}
		})
	}
}

func TestDeliveryPolicyTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	d := newTestDispatcher(t, Options{
		RetryInterval: 10 * time.Millisecond,
		Timeout:       50 * time.Millisecond,
	})
	if err := d.RegisterEndpoint("primary", EndpointConfig{URL: server.URL}); err != nil {
		t.Fatalf("RegisterEndpoint() error = %v", err)
	}
	if err := d.Enqueue("primary", Event{ID: "evt_1", Body: []byte(`{}`)}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		return len(d.DeliveryLog()) >= 1
	})

	got := d.DeliveryLog()[0]
	if got.Success {
		t.Errorf("Success = true, want false on timeout")
	}
	if got.Status != 0 {
		t.Errorf("Status = %d, want 0 on timeout", got.Status)
	}
	if got.Err == nil {
		t.Errorf("Err = nil, want a timeout error")
	}
}

func TestSignatureHeaderIsAttached(t *testing.T) {
	type received struct {
		headerValue string
		body        string
	}
	gotCh := make(chan received, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotCh <- received{headerValue: r.Header.Get("asaas-access-token"), body: string(body)}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	d := newTestDispatcher(t, Options{RetryInterval: 5 * time.Millisecond})
	cfg := EndpointConfig{
		URL:         server.URL,
		HeaderName:  "asaas-access-token",
		HeaderValue: "test-token-123",
	}
	if err := d.RegisterEndpoint("primary", cfg); err != nil {
		t.Fatalf("RegisterEndpoint() error = %v", err)
	}
	if err := d.Enqueue("primary", Event{ID: "evt_1", Body: []byte(`{"hello":"world"}`)}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	select {
	case got := <-gotCh:
		if got.headerValue != "test-token-123" {
			t.Errorf("asaas-access-token header = %q, want %q", got.headerValue, "test-token-123")
		}
		if got.body != `{"hello":"world"}` {
			t.Errorf("request body = %q, want %q", got.body, `{"hello":"world"}`)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received the request")
	}
}

func TestRetryOnFailure(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	d := newTestDispatcher(t, Options{RetryInterval: 10 * time.Millisecond})
	if err := d.RegisterEndpoint("primary", EndpointConfig{URL: server.URL}); err != nil {
		t.Fatalf("RegisterEndpoint() error = %v", err)
	}
	if err := d.Enqueue("primary", Event{ID: "evt_1", Body: []byte(`{}`)}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		return len(d.DeliveryLog()) >= 3
	})

	log := d.DeliveryLog()
	wantStatuses := []int{http.StatusInternalServerError, http.StatusInternalServerError, http.StatusOK}
	for i, want := range wantStatuses {
		if log[i].Status != want {
			t.Errorf("attempt %d status = %d, want %d", i, log[i].Status, want)
		}
	}
	if !log[2].Success {
		t.Errorf("attempt 2 Success = false, want true")
	}

	state, err := d.State("primary")
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if state.Pending != 0 {
		t.Errorf("Pending = %d, want 0 after successful retry", state.Pending)
	}
	if state.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0 after a success", state.ConsecutiveFailures)
	}
}

// TestQueueInterrupt is Fase 1's exit criterion: a test server fails
// every delivery until the endpoint's failure threshold trips the queue
// interruption, no further attempts happen while interrupted, and
// Reactivate drains whatever piled up in the meantime.
func TestQueueInterrupt(t *testing.T) {
	var succeeding atomic.Bool
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if succeeding.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	// FailureThreshold is intentionally left unset here so this test
	// exercises the real DefaultFailureThreshold (15), matching Fase 1's
	// literal exit criterion ("responde 500 quinze vezes"), not a
	// scaled-down stand-in for it.
	const threshold = DefaultFailureThreshold
	d := newTestDispatcher(t, Options{RetryInterval: 5 * time.Millisecond})
	if err := d.RegisterEndpoint("primary", EndpointConfig{URL: server.URL}); err != nil {
		t.Fatalf("RegisterEndpoint() error = %v", err)
	}
	if err := d.Enqueue("primary", Event{ID: "evt_1", Body: []byte(`{}`)}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		state, err := d.State("primary")
		return err == nil && state.Interrupted
	})

	state, err := d.State("primary")
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if state.ConsecutiveFailures != threshold {
		t.Errorf("ConsecutiveFailures = %d, want %d", state.ConsecutiveFailures, threshold)
	}
	if state.Pending != 1 {
		t.Errorf("Pending = %d, want 1: the failing event must stay queued", state.Pending)
	}

	attemptsAtInterruption := requestCount.Load()
	time.Sleep(50 * time.Millisecond)
	if got := requestCount.Load(); got != attemptsAtInterruption {
		t.Errorf("requests kept arriving after interruption: %d -> %d", attemptsAtInterruption, got)
	}
	failureLog := d.DeliveryLog()
	if logLen := len(failureLog); logLen != threshold {
		t.Fatalf("DeliveryLog has %d entries, want exactly %d (no attempts after interruption)", logLen, threshold)
	}
	for i, entry := range failureLog {
		if entry.EventID != "evt_1" {
			t.Errorf("entry %d: EventID = %q, want %q", i, entry.EventID, "evt_1")
		}
		if entry.Status != http.StatusInternalServerError {
			t.Errorf("entry %d: Status = %d, want %d", i, entry.Status, http.StatusInternalServerError)
		}
		if entry.Success {
			t.Errorf("entry %d: Success = true, want false", i)
		}
	}

	succeeding.Store(true)
	if err := d.Reactivate("primary"); err != nil {
		t.Fatalf("Reactivate() error = %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		state, err := d.State("primary")
		return err == nil && state.Pending == 0
	})

	state, err = d.State("primary")
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if state.Interrupted {
		t.Errorf("Interrupted = true after Reactivate and a successful delivery")
	}

	log := d.DeliveryLog()
	last := log[len(log)-1]
	if !last.Success || last.EventID != "evt_1" || last.Status != http.StatusOK {
		t.Errorf("last delivery = %+v, want a successful delivery of evt_1", last)
	}
}

func TestQueueInterruptDoesNotBlockEnqueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	d := newTestDispatcher(t, Options{
		RetryInterval:    5 * time.Millisecond,
		FailureThreshold: 3,
	})
	if err := d.RegisterEndpoint("primary", EndpointConfig{URL: server.URL}); err != nil {
		t.Fatalf("RegisterEndpoint() error = %v", err)
	}
	if err := d.Enqueue("primary", Event{ID: "evt_1", Body: []byte(`{}`)}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		state, err := d.State("primary")
		return err == nil && state.Interrupted
	})

	if err := d.Enqueue("primary", Event{ID: "evt_2", Body: []byte(`{}`)}); err != nil {
		t.Fatalf("Enqueue() while interrupted error = %v", err)
	}

	state, err := d.State("primary")
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if state.Pending != 2 {
		t.Errorf("Pending = %d, want 2: new events accumulate while interrupted", state.Pending)
	}
}

func TestSlowEndpointDoesNotBlockAnotherEndpoint(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(slow.Close)

	fastCh := make(chan struct{}, 1)
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fastCh <- struct{}{}
	}))
	t.Cleanup(fast.Close)

	d := newTestDispatcher(t, Options{
		RetryInterval: 20 * time.Millisecond,
		Timeout:       50 * time.Millisecond,
	})
	if err := d.RegisterEndpoint("slow", EndpointConfig{URL: slow.URL}); err != nil {
		t.Fatalf("RegisterEndpoint(slow) error = %v", err)
	}
	if err := d.RegisterEndpoint("fast", EndpointConfig{URL: fast.URL}); err != nil {
		t.Fatalf("RegisterEndpoint(fast) error = %v", err)
	}
	if err := d.Enqueue("slow", Event{ID: "evt_slow", Body: []byte(`{}`)}); err != nil {
		t.Fatalf("Enqueue(slow) error = %v", err)
	}
	if err := d.Enqueue("fast", Event{ID: "evt_fast", Body: []byte(`{}`)}); err != nil {
		t.Fatalf("Enqueue(fast) error = %v", err)
	}

	select {
	case <-fastCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("fast endpoint never got its request; a slow endpoint must not block others")
	}
}

// TestRegisterEndpointDuringCloseDoesNotPanic guards against a real bug
// caught in review: RegisterEndpoint's wg.Add and Close's wg.Wait raced
// on the same WaitGroup with nothing serializing them, which the Go
// runtime detects and panics the whole process over, not something a
// caller could recover from. Both now share Dispatcher.mu, so Add either
// completes strictly before Close observes the dispatcher as closed, or
// never happens at all.
func TestRegisterEndpointDuringCloseDoesNotPanic(t *testing.T) {
	for i := 0; i < 200; i++ {
		d := New(Options{})

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = d.RegisterEndpoint("racer", EndpointConfig{URL: "http://example.test"})
		}()
		go func() {
			defer wg.Done()
			_ = d.Close()
		}()
		wg.Wait()

		if err := d.Close(); err != nil {
			t.Fatalf("iteration %d: cleanup Close() error = %v", i, err)
		}
	}
}

// TestRedirectResponseCountsAsFailure guards against a real bug caught in
// review: the delivery client used net/http's default redirect policy, so
// a 3xx response was silently followed and the final hop's status got
// recorded instead, hiding a failure FIDELITY.md says must count (any
// non-2xx). The client now stops at the first response via
// http.ErrUseLastResponse.
func TestRedirectResponseCountsAsFailure(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	t.Cleanup(redirecting.Close)

	d := newTestDispatcher(t, Options{RetryInterval: 5 * time.Millisecond})
	if err := d.RegisterEndpoint("primary", EndpointConfig{URL: redirecting.URL}); err != nil {
		t.Fatalf("RegisterEndpoint() error = %v", err)
	}
	if err := d.Enqueue("primary", Event{ID: "evt_1", Body: []byte(`{}`)}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		return len(d.DeliveryLog()) >= 1
	})

	got := d.DeliveryLog()[0]
	if got.Success {
		t.Errorf("Success = true, want false: a 3xx redirect must not be silently followed and treated as success")
	}
	if got.Status != http.StatusFound {
		t.Errorf("Status = %d, want %d: the redirect status itself, not the followed target's", got.Status, http.StatusFound)
	}
}

func TestRegisterEndpointRejectsAfterClose(t *testing.T) {
	d := New(Options{})
	if err := d.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	err := d.RegisterEndpoint("primary", EndpointConfig{URL: "http://example.test"})
	if !errors.Is(err, ErrDispatcherClosed) {
		t.Errorf("RegisterEndpoint() after Close error = %v, want %v", err, ErrDispatcherClosed)
	}
}

func TestRegisterEndpointValidation(t *testing.T) {
	tests := []struct {
		name    string
		epName  string
		cfg     EndpointConfig
		wantErr error
	}{
		{"empty name", "", EndpointConfig{URL: "http://example.test"}, ErrEmptyEndpointName},
		{"empty url", "primary", EndpointConfig{}, ErrEmptyURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newTestDispatcher(t, Options{})
			err := d.RegisterEndpoint(tt.epName, tt.cfg)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("RegisterEndpoint() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegisterEndpointRejectsDuplicateName(t *testing.T) {
	d := newTestDispatcher(t, Options{})
	cfg := EndpointConfig{URL: "http://example.test"}
	if err := d.RegisterEndpoint("primary", cfg); err != nil {
		t.Fatalf("first RegisterEndpoint() error = %v", err)
	}
	if err := d.RegisterEndpoint("primary", cfg); !errors.Is(err, ErrEndpointExists) {
		t.Errorf("second RegisterEndpoint() error = %v, want %v", err, ErrEndpointExists)
	}
}

func TestEnqueueUnknownEndpoint(t *testing.T) {
	d := newTestDispatcher(t, Options{})
	err := d.Enqueue("missing", Event{ID: "evt_1"})
	if !errors.Is(err, ErrEndpointNotFound) {
		t.Errorf("Enqueue() error = %v, want %v", err, ErrEndpointNotFound)
	}
}

func TestEnqueueRejectsEmptyEventID(t *testing.T) {
	d := newTestDispatcher(t, Options{})
	if err := d.RegisterEndpoint("primary", EndpointConfig{URL: "http://example.test"}); err != nil {
		t.Fatalf("RegisterEndpoint() error = %v", err)
	}
	err := d.Enqueue("primary", Event{ID: ""})
	if !errors.Is(err, ErrEmptyEventID) {
		t.Errorf("Enqueue() error = %v, want %v", err, ErrEmptyEventID)
	}
}

func TestReactivateUnknownEndpoint(t *testing.T) {
	d := newTestDispatcher(t, Options{})
	err := d.Reactivate("missing")
	if !errors.Is(err, ErrEndpointNotFound) {
		t.Errorf("Reactivate() error = %v, want %v", err, ErrEndpointNotFound)
	}
}

func TestStateUnknownEndpoint(t *testing.T) {
	d := newTestDispatcher(t, Options{})
	_, err := d.State("missing")
	if !errors.Is(err, ErrEndpointNotFound) {
		t.Errorf("State() error = %v, want %v", err, ErrEndpointNotFound)
	}
}

func TestCloseStopsDelivery(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	d := New(Options{RetryInterval: 5 * time.Millisecond})
	if err := d.RegisterEndpoint("primary", EndpointConfig{URL: server.URL}); err != nil {
		t.Fatalf("RegisterEndpoint() error = %v", err)
	}
	if err := d.Enqueue("primary", Event{ID: "evt_1", Body: []byte(`{}`)}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		return attempts.Load() >= 1
	})

	if err := d.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	after := attempts.Load()
	time.Sleep(50 * time.Millisecond)
	if got := attempts.Load(); got != after {
		t.Errorf("delivery kept happening after Close: %d -> %d", after, got)
	}

	if err := d.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}
