package asaas

import "testing"

func TestReaisToCents(t *testing.T) {
	tests := []struct {
		reais float64
		want  int64
	}{
		{19.90, 1990},
		{0.10, 10},
		{100.00, 10000},
		{0.01, 1},
		{2.50, 250},
		{5.55, 555},
		{9999.99, 999999},
	}
	for _, tt := range tests {
		if got := reaisToCents(tt.reais); got != tt.want {
			t.Errorf("reaisToCents(%v) = %d, want %d", tt.reais, got, tt.want)
		}
	}
}

func TestCentsToReais(t *testing.T) {
	tests := []struct {
		cents int64
		want  float64
	}{
		{1990, 19.90},
		{10, 0.10},
		{10000, 100.00},
		{1, 0.01},
		{555, 5.55},
	}
	for _, tt := range tests {
		if got := centsToReais(tt.cents); got != tt.want {
			t.Errorf("centsToReais(%d) = %v, want %v", tt.cents, got, tt.want)
		}
	}
}
