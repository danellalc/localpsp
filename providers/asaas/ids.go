package asaas

// mintEventID, mintWebhookID and mintAuthToken all pull from the engine's
// own seeded id source, not an unseeded random one: an event id or an
// auto-generated auth token is still part of a delivered webhook's byte
// content, and the project's determinism guarantee ("same seed, same
// sequence of calls, byte-identical payloads") covers it just like any
// entity id.

func (s *Server) mintEventID() string {
	return s.engine.NextID("evt_")
}

func (s *Server) mintWebhookID() string {
	return s.engine.NextID("wh_")
}

func (s *Server) mintAuthToken() string {
	return s.engine.NextToken()
}
