package engine

import "time"

// Customer is the minimal identity a charge or subscription belongs to.
type Customer struct {
	ID        string
	Name      string
	Email     string
	TaxID     string
	CreatedAt time.Time
}
