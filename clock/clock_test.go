package clock_test

import (
	"testing"
	"time"

	"github.com/danellalc/localpsp/clock"
)

func TestClockAdvance(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		advance time.Duration
	}{
		{"advance by one day", 24 * time.Hour},
		{"advance by one hour", time.Hour},
		{"advance by zero", 0},
		{"advance backward", -time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := clock.New(start)
			want := start.Add(tt.advance)

			got := c.Advance(tt.advance)
			if !got.Equal(want) {
				t.Fatalf("Advance(%v) = %v, want %v", tt.advance, got, want)
			}
			if !c.Now().Equal(want) {
				t.Fatalf("Now() after Advance(%v) = %v, want %v", tt.advance, c.Now(), want)
			}
		})
	}
}

func TestClockAdvanceAccumulates(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := clock.New(start)

	c.Advance(24 * time.Hour)
	c.Advance(48 * time.Hour)

	want := start.Add(72 * time.Hour)
	if !c.Now().Equal(want) {
		t.Fatalf("Now() = %v, want %v", c.Now(), want)
	}
}

func TestClockNowDoesNotAdvance(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := clock.New(start)

	first := c.Now()
	second := c.Now()

	if !first.Equal(second) {
		t.Fatalf("Now() changed without Advance: %v then %v", first, second)
	}
}
