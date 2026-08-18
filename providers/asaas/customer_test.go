package asaas

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danellalc/localpsp/dispatch"
)

func TestCreateAndGetCustomer(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})

	resp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/customers", customerRequest{
		Name:    "Ana Silva",
		CPFCNPJ: "123.456.789-01",
		Email:   "ana@example.com",
	})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusOK, resp.body)
	}

	var cust customerResponse
	resp.decode(t, &cust)

	if !strings.HasPrefix(cust.ID, "cus_") {
		t.Errorf("id = %q, want cus_ prefix", cust.ID)
	}
	if cust.Object != "customer" {
		t.Errorf("object = %q, want customer", cust.Object)
	}
	if cust.Name != "Ana Silva" {
		t.Errorf("name = %q, want Ana Silva", cust.Name)
	}
	if cust.CPFCNPJ != "123.456.789-01" {
		t.Errorf("cpfCnpj = %q, want 123.456.789-01", cust.CPFCNPJ)
	}
	if cust.PersonType != "FISICA" {
		t.Errorf("personType = %q, want FISICA", cust.PersonType)
	}
	if _, err := time.Parse(dateTimeLayout, cust.DateCreated); err != nil {
		t.Errorf("dateCreated = %q, does not match layout %q: %v", cust.DateCreated, dateTimeLayout, err)
	}

	getResp := request(t, http.MethodGet, httpSrv.URL+testBasePath+"/customers/"+cust.ID, nil)
	if getResp.status != http.StatusOK {
		t.Fatalf("GET status = %d, want %d: %s", getResp.status, http.StatusOK, getResp.body)
	}
	var fetched customerResponse
	getResp.decode(t, &fetched)
	if fetched.ID != cust.ID {
		t.Errorf("fetched id = %q, want %q", fetched.ID, cust.ID)
	}
	if fetched.DateCreated != cust.DateCreated {
		t.Errorf("fetched dateCreated = %q, want %q", fetched.DateCreated, cust.DateCreated)
	}
}

func TestCreateCustomerJuridica(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})

	resp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/customers", customerRequest{
		Name:    "Empresa LTDA",
		CPFCNPJ: "12345678000199",
	})
	var cust customerResponse
	resp.decode(t, &cust)
	if cust.PersonType != "JURIDICA" {
		t.Errorf("personType = %q, want JURIDICA", cust.PersonType)
	}
}

func TestGetCustomerNotFound(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})

	resp := request(t, http.MethodGet, httpSrv.URL+testBasePath+"/customers/cus_does_not_exist", nil)
	if resp.status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusNotFound)
	}
	var errResp errorResponse
	resp.decode(t, &errResp)
	if len(errResp.Errors) != 1 || errResp.Errors[0].Code != "customer_not_found" {
		t.Errorf("errors = %+v, want a single customer_not_found error", errResp.Errors)
	}
}

func TestCreateCustomerValidation(t *testing.T) {
	tests := []struct {
		name     string
		req      customerRequest
		wantCode string
	}{
		{"missing name", customerRequest{CPFCNPJ: "12345678901"}, "name_required"},
		{"missing cpfCnpj", customerRequest{Name: "Ana"}, "cpf_cnpj_required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, httpSrv := newTestServer(t, 1, dispatch.Options{})
			resp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/customers", tt.req)
			if resp.status != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", resp.status, http.StatusBadRequest)
			}
			var errResp errorResponse
			resp.decode(t, &errResp)
			if len(errResp.Errors) != 1 || errResp.Errors[0].Code != tt.wantCode {
				t.Errorf("errors = %+v, want a single %s error", errResp.Errors, tt.wantCode)
			}
		})
	}
}

func TestConcurrentCustomerCreates(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})

	const n = 20
	ids := make(chan string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			resp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/customers", customerRequest{
				Name:    fmt.Sprintf("Customer %d", i),
				CPFCNPJ: "12345678901",
			})
			if resp.status != http.StatusOK {
				t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
				ids <- ""
				return
			}
			var cust customerResponse
			resp.decode(t, &cust)
			ids <- cust.ID
		}(i)
	}
	wg.Wait()
	close(ids)

	seen := make(map[string]bool)
	for id := range ids {
		if id == "" {
			continue
		}
		if seen[id] {
			t.Errorf("duplicate customer id %q from concurrent creates", id)
		}
		seen[id] = true
	}
	if len(seen) != n {
		t.Errorf("got %d unique customer ids, want %d", len(seen), n)
	}
}
