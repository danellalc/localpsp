package asaas

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/danellalc/localpsp/dispatch"
)

const eventPaymentConfirmed = "PAYMENT_CONFIRMED"

var errWebhookNotFound = errors.New("webhook not found")

type webhookConfig struct {
	ID           string
	URL          string
	Name         string
	Email        string
	AuthToken    string
	APIVersion   string
	SendType     string
	Enabled      bool
	Events       []string
	endpointName string
}

type webhookRegistry struct {
	mu      sync.Mutex
	entries map[string]*webhookConfig
}

func newWebhookRegistry() *webhookRegistry {
	return &webhookRegistry{entries: make(map[string]*webhookConfig)}
}

func (r *webhookRegistry) add(cfg *webhookConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[cfg.ID] = cfg
}

func (r *webhookRegistry) get(id string) (webhookConfig, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, ok := r.entries[id]
	if !ok {
		return webhookConfig{}, false
	}
	return *cfg, true
}

// update applies fn to the named webhook's config while holding the
// registry lock for fn's entire duration. Callers that also need to push
// the change into dispatch (a second store with its own lock) must do
// that from inside fn, not after update returns: otherwise two concurrent
// updates can interleave their registry write and their dispatch write,
// leaving the two stores permanently disagreeing about the same webhook.
func (r *webhookRegistry) update(id string, fn func(cfg *webhookConfig) error) (webhookConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, ok := r.entries[id]
	if !ok {
		return webhookConfig{}, errWebhookNotFound
	}
	if err := fn(cfg); err != nil {
		return webhookConfig{}, err
	}
	return *cfg, nil
}

func (r *webhookRegistry) subscribers(eventType string) []webhookConfig {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []webhookConfig
	for _, cfg := range r.entries {
		if !cfg.Enabled {
			continue
		}
		if wantsEvent(cfg.Events, eventType) {
			out = append(out, *cfg)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func wantsEvent(events []string, eventType string) bool {
	if len(events) == 0 {
		return true
	}
	for _, e := range events {
		if e == eventType {
			return true
		}
	}
	return false
}

type eventEnvelope struct {
	ID          string          `json:"id"`
	Event       string          `json:"event"`
	DateCreated string          `json:"dateCreated"`
	Payment     paymentResponse `json:"payment"`
}

func (s *Server) fireEvent(eventType string, payment paymentResponse) {
	envelope := eventEnvelope{
		ID:          s.mintEventID(),
		Event:       eventType,
		DateCreated: formatDateTime(s.engine.Now()),
		Payment:     payment,
	}
	body, _ := json.Marshal(envelope)

	for _, sub := range s.webhooks.subscribers(eventType) {
		_ = s.dispatch.Enqueue(sub.endpointName, dispatch.Event{ID: envelope.ID, Body: body})
	}
}

type webhookRequest struct {
	URL        string   `json:"url"`
	Name       string   `json:"name,omitempty"`
	Email      string   `json:"email,omitempty"`
	AuthToken  string   `json:"authToken,omitempty"`
	APIVersion string   `json:"apiVersion,omitempty"`
	SendType   string   `json:"sendType,omitempty"`
	Enabled    *bool    `json:"enabled,omitempty"`
	Events     []string `json:"events,omitempty"`
}

type webhookUpdateRequest struct {
	URL         *string  `json:"url,omitempty"`
	Name        *string  `json:"name,omitempty"`
	Email       *string  `json:"email,omitempty"`
	AuthToken   *string  `json:"authToken,omitempty"`
	APIVersion  *string  `json:"apiVersion,omitempty"`
	SendType    *string  `json:"sendType,omitempty"`
	Enabled     *bool    `json:"enabled,omitempty"`
	Interrupted *bool    `json:"interrupted,omitempty"`
	Events      []string `json:"events,omitempty"`
}

type webhookResponse struct {
	ID          string   `json:"id"`
	URL         string   `json:"url"`
	Name        string   `json:"name,omitempty"`
	Email       string   `json:"email,omitempty"`
	AuthToken   string   `json:"authToken"`
	APIVersion  string   `json:"apiVersion"`
	SendType    string   `json:"sendType"`
	Enabled     bool     `json:"enabled"`
	Interrupted bool     `json:"interrupted"`
	Events      []string `json:"events"`
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	var req webhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		writeError(w, http.StatusBadRequest, "url_required", "url is required")
		return
	}

	authToken := req.AuthToken
	if authToken == "" {
		authToken = s.mintAuthToken()
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	apiVersion := req.APIVersion
	if apiVersion == "" {
		apiVersion = "3"
	}
	sendType := req.SendType
	if sendType == "" {
		sendType = "SEQUENTIALLY"
	}

	cfg := &webhookConfig{
		ID:         s.mintWebhookID(),
		URL:        req.URL,
		Name:       req.Name,
		Email:      req.Email,
		AuthToken:  authToken,
		APIVersion: apiVersion,
		SendType:   sendType,
		Enabled:    enabled,
		Events:     req.Events,
	}
	cfg.endpointName = cfg.ID

	if err := s.dispatch.RegisterEndpoint(cfg.endpointName, dispatch.EndpointConfig{
		URL:         cfg.URL,
		HeaderName:  "asaas-access-token",
		HeaderValue: cfg.AuthToken,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not register webhook endpoint")
		return
	}

	s.webhooks.add(cfg)

	writeJSON(w, http.StatusOK, s.newWebhookResponse(*cfg))
}

func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	existing, ok := s.webhooks.get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "webhook_not_found", "webhook not found")
		return
	}

	var req webhookUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	if req.URL != nil && strings.TrimSpace(*req.URL) == "" {
		writeError(w, http.StatusBadRequest, "url_required", "url must not be empty")
		return
	}

	updated, err := s.webhooks.update(id, func(cfg *webhookConfig) error {
		if req.URL != nil {
			cfg.URL = *req.URL
		}
		if req.Name != nil {
			cfg.Name = *req.Name
		}
		if req.Email != nil {
			cfg.Email = *req.Email
		}
		if req.AuthToken != nil {
			cfg.AuthToken = *req.AuthToken
		}
		if req.APIVersion != nil {
			cfg.APIVersion = *req.APIVersion
		}
		if req.SendType != nil {
			cfg.SendType = *req.SendType
		}
		if req.Enabled != nil {
			cfg.Enabled = *req.Enabled
		}
		if req.Events != nil {
			cfg.Events = req.Events
		}

		// Pushing the dispatch update from inside the registry's own
		// locked callback keeps the two stores from ever disagreeing:
		// a concurrent update can't interleave its own registry write
		// and dispatch write between this update's two writes, because
		// both happen under the same registry lock.
		if req.URL != nil || req.AuthToken != nil {
			if err := s.dispatch.UpdateEndpoint(cfg.endpointName, dispatch.EndpointConfig{
				URL:         cfg.URL,
				HeaderName:  "asaas-access-token",
				HeaderValue: cfg.AuthToken,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errWebhookNotFound) {
			writeError(w, http.StatusNotFound, "webhook_not_found", "webhook not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "could not update webhook endpoint")
		return
	}

	if req.Interrupted != nil && !*req.Interrupted {
		if err := s.dispatch.Reactivate(existing.endpointName); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "could not reactivate webhook queue")
			return
		}
	}

	writeJSON(w, http.StatusOK, s.newWebhookResponse(updated))
}

func (s *Server) newWebhookResponse(cfg webhookConfig) webhookResponse {
	interrupted := false
	if state, err := s.dispatch.State(cfg.endpointName); err == nil {
		interrupted = state.Interrupted
	}
	events := cfg.Events
	if events == nil {
		events = []string{}
	}
	return webhookResponse{
		ID:          cfg.ID,
		URL:         cfg.URL,
		Name:        cfg.Name,
		Email:       cfg.Email,
		AuthToken:   cfg.AuthToken,
		APIVersion:  cfg.APIVersion,
		SendType:    cfg.SendType,
		Enabled:     cfg.Enabled,
		Interrupted: interrupted,
		Events:      events,
	}
}
