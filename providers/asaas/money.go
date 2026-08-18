package asaas

import "math"

func reaisToCents(value float64) int64 {
	return int64(math.Round(value * 100))
}

func centsToReais(cents int64) float64 {
	return float64(cents) / 100
}
