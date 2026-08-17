package dispatch

import "fmt"

// EndpointConfig describes a webhook destination: where to deliver and
// which header carries the configured signing value. This is the
// mechanism behind Asaas's "signature": a static configured token, sent
// back verbatim in a fixed header, not a computed HMAC. The facade
// supplies the provider-specific header name and token; dispatch only
// knows how to attach whatever header it's given.
type EndpointConfig struct {
	URL         string
	HeaderName  string
	HeaderValue string
}

// EndpointState is a snapshot of one endpoint's delivery queue: the
// material a later `localpsp state` command shows.
type EndpointState struct {
	Interrupted         bool
	ConsecutiveFailures int
	Pending             int
}

// endpointState is the live, mutable version of EndpointState, guarded
// by Dispatcher.mu.
type endpointState struct {
	config              EndpointConfig
	pending             []Event
	consecutiveFailures int
	interrupted         bool
	wake                chan struct{}
	syntheticFailures   int
}

// RegisterEndpoint adds a named webhook destination and starts its
// background delivery loop. Endpoint names must be unique within a
// Dispatcher; an Asaas account can configure more than one webhook, and
// each gets its own queue and failure count.
func (d *Dispatcher) RegisterEndpoint(name string, cfg EndpointConfig) error {
	if name == "" {
		return ErrEmptyEndpointName
	}
	if cfg.URL == "" {
		return ErrEmptyURL
	}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return ErrDispatcherClosed
	}
	if _, exists := d.endpoints[name]; exists {
		d.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrEndpointExists, name)
	}
	ep := &endpointState{config: cfg, wake: make(chan struct{}, 1)}
	d.endpoints[name] = ep
	// Add happens inside the same critical section that just checked
	// closed, so it can never land concurrently with Close's Wait: Close
	// can't observe closed as false and proceed past its own mu.Lock
	// until this section (and its Add) has finished.
	d.wg.Add(1)
	d.mu.Unlock()

	go d.run(name, ep)
	return nil
}

// Enqueue schedules an event for delivery to the named endpoint and
// returns immediately; delivery happens in the background. If the
// endpoint's queue is currently interrupted, the event still gets queued
// and stays pending until Reactivate is called, exactly like production
// Asaas.
func (d *Dispatcher) Enqueue(name string, ev Event) error {
	if ev.ID == "" {
		return ErrEmptyEventID
	}

	d.mu.Lock()
	ep, ok := d.endpoints[name]
	if !ok {
		d.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrEndpointNotFound, name)
	}
	ep.pending = append(ep.pending, ev)
	d.mu.Unlock()

	wake(ep)
	return nil
}

// UpdateEndpoint replaces an already registered endpoint's delivery
// configuration (URL and signing header) without touching its pending
// queue, failure count or interrupted state.
func (d *Dispatcher) UpdateEndpoint(name string, cfg EndpointConfig) error {
	if cfg.URL == "" {
		return ErrEmptyURL
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	ep, ok := d.endpoints[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrEndpointNotFound, name)
	}
	ep.config = cfg
	return nil
}

// Reactivate clears an endpoint's interrupted flag and resumes delivery
// of its pending events, in the order they were originally enqueued.
func (d *Dispatcher) Reactivate(name string) error {
	d.mu.Lock()
	ep, ok := d.endpoints[name]
	if !ok {
		d.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrEndpointNotFound, name)
	}
	ep.interrupted = false
	ep.consecutiveFailures = 0
	d.mu.Unlock()

	wake(ep)
	return nil
}

// InjectFailures sets how many of the named endpoint's next delivery
// attempts fail synthetically: each one goes through the exact same
// recordAttempt/finishAttempt path a real failure takes (so it counts
// toward the consecutive-failure streak and can trip queue interruption
// just like a real outage), but never makes a real HTTP call. It's the
// mechanism behind chaos retry-storm and chaos queue-interrupt. Once the
// counter reaches zero, delivery resumes making real HTTP calls. Calling
// it again before the counter runs out replaces the remaining count
// rather than adding to it.
func (d *Dispatcher) InjectFailures(name string, n int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	ep, ok := d.endpoints[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrEndpointNotFound, name)
	}
	ep.syntheticFailures = n
	return nil
}

// State returns a snapshot of the named endpoint's queue: whether it's
// interrupted, its current consecutive failure count and how many events
// are still pending delivery.
func (d *Dispatcher) State(name string) (EndpointState, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ep, ok := d.endpoints[name]
	if !ok {
		return EndpointState{}, fmt.Errorf("%w: %q", ErrEndpointNotFound, name)
	}
	return EndpointState{
		Interrupted:         ep.interrupted,
		ConsecutiveFailures: ep.consecutiveFailures,
		Pending:             len(ep.pending),
	}, nil
}

// wake nudges an endpoint's delivery loop without blocking: a pending
// signal is enough, a queued second one is redundant since the loop
// always re-reads the real pending list.
func wake(ep *endpointState) {
	select {
	case ep.wake <- struct{}{}:
	default:
	}
}
