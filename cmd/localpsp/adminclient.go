package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultAddr matches serve's own default listen address (":8420"),
// resolved the same way serve prints it as reachable at.
const defaultAddr = "http://localhost:8420"

// requestTimeout bounds how long the CLI waits for a single admin API
// call, so a hung server never leaves the command stuck forever.
const requestTimeout = 10 * time.Second

// normalizeAddr makes sure addr carries a scheme, so a user can pass
// either "localhost:8420" or the full "http://localhost:8420" to --addr.
func normalizeAddr(addr string) string {
	addr = strings.TrimSuffix(addr, "/")
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	return "http://" + addr
}

// adminErrorBody mirrors the facade's own {"errors": [...]} shape well
// enough to pull out a human description, without importing the facade
// package just for one struct.
type adminErrorBody struct {
	Errors []struct {
		Description string `json:"description"`
	} `json:"errors"`
}

// adminRequest calls localpsp's admin API and decodes a successful JSON
// response into out (which may be nil for calls with no useful body). A
// connection failure comes back as a clear, human error instead of a raw
// Go network error, and a non-2xx response comes back with the facade's
// own error description instead of a bare status code.
func adminRequest(ctx context.Context, addr, method, path string, body, out any) error {
	base := normalizeAddr(addr)

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach localpsp at %s: is `localpsp serve` running there?", base)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response from %s: %w", base, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr adminErrorBody
		if jsonErr := json.Unmarshal(data, &apiErr); jsonErr == nil && len(apiErr.Errors) > 0 {
			return fmt.Errorf("localpsp rejected the request: %s", apiErr.Errors[0].Description)
		}
		return fmt.Errorf("localpsp returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", base, err)
	}
	return nil
}
