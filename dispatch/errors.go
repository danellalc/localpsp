package dispatch

import "errors"

// Sentinel errors returned by the dispatcher. Wrap these with fmt.Errorf
// and %w so callers can match with errors.Is.
var (
	ErrEndpointNotFound  = errors.New("endpoint not found")
	ErrEndpointExists    = errors.New("endpoint already registered")
	ErrEmptyEndpointName = errors.New("endpoint name must not be empty")
	ErrEmptyURL          = errors.New("endpoint url must not be empty")
	ErrEmptyEventID      = errors.New("event id must not be empty")
	ErrDispatcherClosed  = errors.New("dispatcher is closed")
)
