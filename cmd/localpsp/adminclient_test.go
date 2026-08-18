package main

import "testing"

func TestNormalizeAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{"bare host and port gets http scheme", "localhost:8420", "http://localhost:8420"},
		{"http scheme is left alone", "http://localhost:8420", "http://localhost:8420"},
		{"https scheme is left alone", "https://example.test:8420", "https://example.test:8420"},
		{"trailing slash is trimmed", "http://localhost:8420/", "http://localhost:8420"},
		{"bare host with trailing slash gets scheme and trim", "localhost:8420/", "http://localhost:8420"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeAddr(tt.addr); got != tt.want {
				t.Errorf("normalizeAddr(%q) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}
