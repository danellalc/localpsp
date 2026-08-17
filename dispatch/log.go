package dispatch

import "time"

// DeliveryAttempt records the outcome of a single delivery attempt,
// success or failure.
type DeliveryAttempt struct {
	EventID  string
	Endpoint string
	At       time.Time
	// Status is the HTTP status code received, or 0 if none was
	// received at all (timeout or connection error, see Err).
	Status  int
	Success bool
	// Err is set when no status code was received: a timeout, a
	// connection error, the Dispatcher shutting down mid-request, or (see
	// ErrSimulatedFailure) a chaos-injected synthetic failure that never
	// touched the network at all.
	Err error
}

// recordAttempt appends a to the delivery log.
func (d *Dispatcher) recordAttempt(a DeliveryAttempt) {
	d.mu.Lock()
	d.log = append(d.log, a)
	d.mu.Unlock()
}

// DeliveryLog returns every delivery attempt recorded so far, oldest
// first. It's the material a later `localpsp state` command shows.
func (d *Dispatcher) DeliveryLog() []DeliveryAttempt {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]DeliveryAttempt, len(d.log))
	copy(out, d.log)
	return out
}
