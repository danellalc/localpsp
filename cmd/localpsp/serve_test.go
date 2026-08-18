package main

import "testing"

func TestPublicURLFor(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{"bare port binds every interface, reachable at localhost", ":8420", "http://localhost:8420"},
		{"explicit 0.0.0.0 is also every interface", "0.0.0.0:8420", "http://localhost:8420"},
		{"explicit IPv6 any is also every interface", "[::]:8420", "http://localhost:8420"},
		{"specific host is kept as-is", "127.0.0.1:8420", "http://127.0.0.1:8420"},
		{"unparseable addr falls back to a plain http prefix", "not-a-valid-addr", "http://not-a-valid-addr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := publicURLFor(tt.addr); got != tt.want {
				t.Errorf("publicURLFor(%q) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}
