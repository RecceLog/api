package domain

import (
	"errors"

	"github.com/google/uuid"
)

// Waypoint is a user-defined point on the route.
// It can be used to enforce a certain path or to specify a point of interest.
// A waypoint must have:
//   - a position
//   - an order
type Waypoint struct {
	ID       uuid.UUID
	Position Point
	Order    int
	Name     string
}

// Validate that the position is a valid coordinate and that order is a positive number
func (w Waypoint) Validate() error {
	var errs []error
	if !w.Position.Valid() {
		errs = append(errs, invalid("position", "invalid coordinate"))
	}
	if w.Order < 0 {
		errs = append(errs, invalid("order", "can't be negative"))
	}
	return errors.Join(errs...)
}
