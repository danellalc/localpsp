package asaas

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/danellalc/localpsp/engine"
)

// customerProfile holds the request fields engine.Customer has no reason
// to know about: they never affect a charge or subscription's behavior,
// only what a GET echoes back. Keeping them here, not in engine, matches
// engine.Customer's own "minimal identity" design instead of bloating it
// with presentation-only data.
type customerProfile struct {
	Phone                string
	MobilePhone          string
	Address              string
	AddressNumber        string
	Complement           string
	Province             string
	PostalCode           string
	ExternalReference    string
	NotificationDisabled bool
	AdditionalEmails     string
	MunicipalInscription string
	StateInscription     string
	Observations         string
	GroupName            string
	Company              string
	ForeignCustomer      bool
}

type customerProfileStore struct {
	mu   sync.Mutex
	byID map[string]customerProfile
}

func newCustomerProfileStore() *customerProfileStore {
	return &customerProfileStore{byID: make(map[string]customerProfile)}
}

func (s *customerProfileStore) set(id string, p customerProfile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[id] = p
}

func (s *customerProfileStore) get(id string) customerProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[id]
}

type customerRequest struct {
	Name                 string `json:"name"`
	CPFCNPJ              string `json:"cpfCnpj"`
	Email                string `json:"email,omitempty"`
	Phone                string `json:"phone,omitempty"`
	MobilePhone          string `json:"mobilePhone,omitempty"`
	Address              string `json:"address,omitempty"`
	AddressNumber        string `json:"addressNumber,omitempty"`
	Complement           string `json:"complement,omitempty"`
	Province             string `json:"province,omitempty"`
	PostalCode           string `json:"postalCode,omitempty"`
	ExternalReference    string `json:"externalReference,omitempty"`
	NotificationDisabled bool   `json:"notificationDisabled,omitempty"`
	AdditionalEmails     string `json:"additionalEmails,omitempty"`
	MunicipalInscription string `json:"municipalInscription,omitempty"`
	StateInscription     string `json:"stateInscription,omitempty"`
	Observations         string `json:"observations,omitempty"`
	GroupName            string `json:"groupName,omitempty"`
	Company              string `json:"company,omitempty"`
	ForeignCustomer      bool   `json:"foreignCustomer,omitempty"`
}

type customerResponse struct {
	Object      string `json:"object"`
	ID          string `json:"id"`
	DateCreated string `json:"dateCreated"`
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	CPFCNPJ     string `json:"cpfCnpj"`
	PersonType  string `json:"personType"`
	Deleted     bool   `json:"deleted"`

	Phone         string `json:"phone,omitempty"`
	MobilePhone   string `json:"mobilePhone,omitempty"`
	Address       string `json:"address,omitempty"`
	AddressNumber string `json:"addressNumber,omitempty"`
	Complement    string `json:"complement,omitempty"`
	Province      string `json:"province,omitempty"`
	PostalCode    string `json:"postalCode,omitempty"`
	// City, CityName, State and Country are never populated: the real
	// Asaas API derives them from postalCode via its own CEP lookup
	// service, and localpsp never touches real external services. See
	// FIDELITY.md.
	ExternalReference    string `json:"externalReference,omitempty"`
	NotificationDisabled bool   `json:"notificationDisabled,omitempty"`
	AdditionalEmails     string `json:"additionalEmails,omitempty"`
	MunicipalInscription string `json:"municipalInscription,omitempty"`
	StateInscription     string `json:"stateInscription,omitempty"`
	Observations         string `json:"observations,omitempty"`
	GroupName            string `json:"groupName,omitempty"`
	Company              string `json:"company,omitempty"`
	ForeignCustomer      bool   `json:"foreignCustomer,omitempty"`
}

func (s *Server) handleCreateCustomer(w http.ResponseWriter, r *http.Request) {
	var req customerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name_required", "name is required")
		return
	}
	if strings.TrimSpace(req.CPFCNPJ) == "" {
		writeError(w, http.StatusBadRequest, "cpf_cnpj_required", "cpfCnpj is required")
		return
	}

	cust, err := s.engine.CreateCustomer(r.Context(), engine.CreateCustomerInput{
		Name:  req.Name,
		Email: req.Email,
		TaxID: req.CPFCNPJ,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}

	s.customerProfiles.set(cust.ID, customerProfile{
		Phone:                req.Phone,
		MobilePhone:          req.MobilePhone,
		Address:              req.Address,
		AddressNumber:        req.AddressNumber,
		Complement:           req.Complement,
		Province:             req.Province,
		PostalCode:           req.PostalCode,
		ExternalReference:    req.ExternalReference,
		NotificationDisabled: req.NotificationDisabled,
		AdditionalEmails:     req.AdditionalEmails,
		MunicipalInscription: req.MunicipalInscription,
		StateInscription:     req.StateInscription,
		Observations:         req.Observations,
		GroupName:            req.GroupName,
		Company:              req.Company,
		ForeignCustomer:      req.ForeignCustomer,
	})

	writeJSON(w, http.StatusOK, s.newCustomerResponse(cust))
}

func (s *Server) handleGetCustomer(w http.ResponseWriter, r *http.Request) {
	cust, err := s.engine.GetCustomer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.newCustomerResponse(cust))
}

func (s *Server) newCustomerResponse(c *engine.Customer) customerResponse {
	p := s.customerProfiles.get(c.ID)
	return customerResponse{
		Object:      "customer",
		ID:          c.ID,
		DateCreated: formatDateTime(c.CreatedAt),
		Name:        c.Name,
		Email:       c.Email,
		CPFCNPJ:     c.TaxID,
		PersonType:  personType(c.TaxID),
		Deleted:     false,

		Phone:                p.Phone,
		MobilePhone:          p.MobilePhone,
		Address:              p.Address,
		AddressNumber:        p.AddressNumber,
		Complement:           p.Complement,
		Province:             p.Province,
		PostalCode:           p.PostalCode,
		ExternalReference:    p.ExternalReference,
		NotificationDisabled: p.NotificationDisabled,
		AdditionalEmails:     p.AdditionalEmails,
		MunicipalInscription: p.MunicipalInscription,
		StateInscription:     p.StateInscription,
		Observations:         p.Observations,
		GroupName:            p.GroupName,
		Company:              p.Company,
		ForeignCustomer:      p.ForeignCustomer,
	}
}

func personType(taxID string) string {
	if len(digitsOnly(taxID)) > 11 {
		return "JURIDICA"
	}
	return "FISICA"
}

func digitsOnly(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b = append(b, s[i])
		}
	}
	return string(b)
}
