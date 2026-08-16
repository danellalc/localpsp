package dispatch

// Event is an opaque outbound webhook payload. Dispatch never inspects
// Body: it POSTs it verbatim and uses ID only for its own bookkeeping
// (the delivery log, and later the chaos duplicate-delivery scenario,
// which resends an event with the same ID on purpose).
type Event struct {
	ID   string
	Body []byte
}
