package dispatch

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestInjectFailuresNeverReachesRealServer is chaos retry-storm's core
// promise: a run of injected failures produces exactly that many failed
// log entries, none of which ever touch the real endpoint, and the
// attempt right after the counter runs out is a real HTTP call that can
// succeed.
func TestInjectFailuresNeverReachesRealServer(t *testing.T) {
	tests := []struct {
		name     string
		injected int
	}{
		{"one injected failure", 1},
		{"several injected failures", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestCount atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(server.Close)

			// RetryInterval is generous here (not the usual few
			// milliseconds), so there's a real window to assert the
			// request count right after the last injected failure lands,
			// before the loop's retry sleep even ends and a real attempt
			// could start.
			d := newTestDispatcher(t, Options{RetryInterval: 300 * time.Millisecond})
			if err := d.RegisterEndpoint("primary", EndpointConfig{URL: server.URL}); err != nil {
				t.Fatalf("RegisterEndpoint() error = %v", err)
			}
			if err := d.InjectFailures("primary", tt.injected); err != nil {
				t.Fatalf("InjectFailures() error = %v", err)
			}
			if err := d.Enqueue("primary", Event{ID: "evt_1", Body: []byte(`{}`)}); err != nil {
				t.Fatalf("Enqueue() error = %v", err)
			}

			waitFor(t, 2*time.Second, func() bool {
				return len(d.DeliveryLog()) >= tt.injected
			})

			log := d.DeliveryLog()
			if got := requestCount.Load(); got != 0 {
				t.Fatalf("requestCount = %d during injected failures, want 0: a synthetic failure must never reach the real server", got)
			}
			for i := 0; i < tt.injected; i++ {
				if log[i].Success {
					t.Errorf("entry %d: Success = true, want false (synthetic failure)", i)
				}
				if log[i].Status != 0 {
					t.Errorf("entry %d: Status = %d, want 0: no real HTTP call was made", i, log[i].Status)
				}
				if !errors.Is(log[i].Err, ErrSimulatedFailure) {
					t.Errorf("entry %d: Err = %v, want ErrSimulatedFailure", i, log[i].Err)
				}
			}

			waitFor(t, 2*time.Second, func() bool {
				return len(d.DeliveryLog()) > tt.injected
			})
			final := d.DeliveryLog()[tt.injected]
			if !final.Success {
				t.Errorf("attempt after the injected run Success = false, want true (a real HTTP call)")
			}
			if final.Status != http.StatusOK {
				t.Errorf("attempt after the injected run Status = %d, want %d", final.Status, http.StatusOK)
			}
			if got := requestCount.Load(); got != 1 {
				t.Errorf("requestCount = %d after the real attempt, want 1", got)
			}
		})
	}
}

// TestInjectFailuresTripsQueueInterruption is chaos queue-interrupt's core
// promise: injecting exactly the failure threshold's worth of synthetic
// failures interrupts the queue for real, the same as a real outage would,
// without a single request reaching the target server.
func TestInjectFailuresTripsQueueInterruption(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	const threshold = 5
	d := newTestDispatcher(t, Options{RetryInterval: 2 * time.Millisecond, FailureThreshold: threshold})
	if err := d.RegisterEndpoint("primary", EndpointConfig{URL: server.URL}); err != nil {
		t.Fatalf("RegisterEndpoint() error = %v", err)
	}
	if err := d.InjectFailures("primary", threshold); err != nil {
		t.Fatalf("InjectFailures() error = %v", err)
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
		t.Errorf("Pending = %d, want 1: the event must stay queued behind the interruption", state.Pending)
	}
	if got := requestCount.Load(); got != 0 {
		t.Errorf("requestCount = %d, want 0: interruption was tripped by synthetic failures alone", got)
	}

	log := d.DeliveryLog()
	if len(log) != threshold {
		t.Fatalf("DeliveryLog has %d entries, want exactly %d", len(log), threshold)
	}
	for i, entry := range log {
		if !errors.Is(entry.Err, ErrSimulatedFailure) {
			t.Errorf("entry %d: Err = %v, want ErrSimulatedFailure", i, entry.Err)
		}
	}
}

func TestInjectFailuresUnknownEndpoint(t *testing.T) {
	d := newTestDispatcher(t, Options{})
	err := d.InjectFailures("missing", 3)
	if !errors.Is(err, ErrEndpointNotFound) {
		t.Errorf("InjectFailures() error = %v, want %v", err, ErrEndpointNotFound)
	}
}
