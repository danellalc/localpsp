// Package dispatch delivers webhook events over real HTTP, in real wall
// clock time, unlike engine's virtual clock. It knows nothing about
// payment domain concepts or any provider's JSON shape: it moves an
// opaque Event to a configured endpoint and tracks delivery outcomes,
// retries and Asaas's queue interruption semantics. A provider facade
// sits on top and translates its own events into the Event this package
// delivers.
package dispatch

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"time"
)

// DefaultRetryInterval is the documented placeholder for the wait between
// delivery retries, until the exact spacing is confirmed against Asaas.
// See FIDELITY.md.
const DefaultRetryInterval = 30 * time.Second

// DefaultFailureThreshold is how many consecutive delivery failures pause
// an endpoint's queue, mirroring Asaas's real behavior.
const DefaultFailureThreshold = 15

// DefaultTimeout is how long a single delivery attempt waits for a
// response before counting it as a failure, mirroring Asaas's real 10
// second window.
const DefaultTimeout = 10 * time.Second

// Options configures a new Dispatcher. The zero value of every field
// falls back to its documented default.
type Options struct {
	// RetryInterval is the wait before retrying an event that just
	// failed to deliver. Defaults to DefaultRetryInterval.
	RetryInterval time.Duration
	// FailureThreshold is how many consecutive failures interrupt an
	// endpoint's queue. Defaults to DefaultFailureThreshold.
	FailureThreshold int
	// Timeout bounds how long a single delivery attempt waits for a
	// response. Defaults to DefaultTimeout.
	Timeout time.Duration
}

// Dispatcher delivers Events to registered endpoints in the background,
// retrying failures and pausing an endpoint's queue after too many
// consecutive failures happen in a row. A Dispatcher must be closed with
// Close once it's no longer needed, or its delivery goroutines leak.
type Dispatcher struct {
	retryInterval    time.Duration
	failureThreshold int
	client           *http.Client

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu        sync.Mutex
	closed    bool
	endpoints map[string]*endpointState
	log       []DeliveryAttempt
}

// New builds a Dispatcher and starts delivering to endpoints registered
// on it as soon as events are enqueued.
func New(opts Options) *Dispatcher {
	retryInterval := opts.RetryInterval
	if retryInterval <= 0 {
		retryInterval = DefaultRetryInterval
	}
	failureThreshold := opts.FailureThreshold
	if failureThreshold <= 0 {
		failureThreshold = DefaultFailureThreshold
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Dispatcher{
		retryInterval:    retryInterval,
		failureThreshold: failureThreshold,
		client: &http.Client{
			Timeout:   timeout,
			Transport: http.DefaultTransport.(*http.Transport).Clone(),
			// A redirect must not be silently followed: the 3xx status
			// itself has to reach the >=200 and <300 success check below,
			// exactly like a real endpoint answering with any other
			// non-2xx status.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		ctx:       ctx,
		cancel:    cancel,
		endpoints: make(map[string]*endpointState),
	}
}

// Close stops every background delivery loop, waits for them to return
// and releases pooled connections. It's safe to call more than once.
func (d *Dispatcher) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	d.mu.Unlock()

	d.cancel()
	d.wg.Wait()
	d.client.CloseIdleConnections()
	return nil
}

// deliver POSTs ev.Body to cfg.URL with the configured signing header
// attached, and reports the resulting status code. err is non-nil only
// when no status code was received at all (timeout, connection error, a
// canceled Dispatcher).
func (d *Dispatcher) deliver(cfg EndpointConfig, ev Event) (status int, err error) {
	req, err := http.NewRequestWithContext(d.ctx, http.MethodPost, cfg.URL, bytes.NewReader(ev.Body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.HeaderName != "" {
		req.Header.Set(cfg.HeaderName, cfg.HeaderValue)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	return resp.StatusCode, nil
}
