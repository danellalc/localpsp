package dispatch

import "time"

// run is one endpoint's background delivery loop. It sleeps whenever
// there's nothing to do (queue empty or interrupted) and wakes on
// Enqueue, Reactivate or Dispatcher.Close. A slow or hanging endpoint
// only blocks its own loop: every endpoint gets its own goroutine, so
// other endpoints and callers of Enqueue are never affected.
func (d *Dispatcher) run(name string, ep *endpointState) {
	defer d.wg.Done()

	for {
		ev, cfg, synthetic, ok := d.nextDelivery(ep)
		if !ok {
			select {
			case <-ep.wake:
				continue
			case <-d.ctx.Done():
				return
			}
		}

		var status int
		var err error
		if synthetic {
			err = ErrSimulatedFailure
		} else {
			status, err = d.deliver(cfg, ev)
		}
		success := err == nil && status >= 200 && status < 300

		d.recordAttempt(DeliveryAttempt{
			EventID:  ev.ID,
			Endpoint: name,
			At:       time.Now(),
			Status:   status,
			Success:  success,
			Err:      err,
		})

		if interrupted := d.finishAttempt(ep, success); interrupted || success {
			continue
		}

		select {
		case <-time.After(d.retryInterval):
		case <-d.ctx.Done():
			return
		}
	}
}

// nextDelivery returns the event at the head of ep's queue, unless the
// endpoint is interrupted or has nothing pending. synthetic reports
// whether this attempt owes a chaos-injected failure instead of a real
// HTTP call; when it does, one is consumed from ep's counter in the same
// locked section that hands out the event, so this decision can never
// race a concurrent InjectFailures call or land on an attempt that's
// already in flight.
func (d *Dispatcher) nextDelivery(ep *endpointState) (ev Event, cfg EndpointConfig, synthetic, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if ep.interrupted || len(ep.pending) == 0 {
		return Event{}, EndpointConfig{}, false, false
	}
	if ep.syntheticFailures > 0 {
		ep.syntheticFailures--
		synthetic = true
	}
	return ep.pending[0], ep.config, synthetic, true
}

// finishAttempt applies the outcome of a delivery attempt to ep's state:
// a success pops the event and resets the failure streak, a failure
// extends the streak and interrupts the queue once it reaches the
// configured threshold. It reports whether this attempt just interrupted
// the queue.
func (d *Dispatcher) finishAttempt(ep *endpointState, success bool) (interrupted bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if success {
		if len(ep.pending) > 0 {
			ep.pending = ep.pending[1:]
		}
		ep.consecutiveFailures = 0
		return false
	}

	ep.consecutiveFailures++
	if ep.consecutiveFailures >= d.failureThreshold {
		ep.interrupted = true
		return true
	}
	return false
}
