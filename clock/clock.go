// Package clock provides a virtual clock that only moves forward on an
// explicit call, never on its own. The engine uses it instead of time.Now
// so that payment lifecycles stay deterministic and testable without real
// waiting.
package clock

import (
	"sync"
	"time"
)

// Clock is a virtual clock. Its value only changes through Advance.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// New builds a Clock starting at the given time.
func New(start time.Time) *Clock {
	return &Clock{now: start}
}

// Now returns the clock's current time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d and returns the new current time.
// A negative d is allowed and moves the clock backward.
func (c *Clock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}
