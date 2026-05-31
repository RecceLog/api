package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Vehicle represents how a Route can be taken.
type Vehicle string

const (
	VehicleFoot       Vehicle = "FOOT"
	VehicleBike       Vehicle = "BIKE"
	VehicleMotorcycle Vehicle = "MOTORCYCLE"
	VehicleCar        Vehicle = "CAR"
)

// Valid verifies that the vehicle is a known value.
func (v Vehicle) Valid() bool {
	switch v {
	case VehicleFoot, VehicleBike, VehicleMotorcycle, VehicleCar:
		return true
	default:
		return false
	}
}

// Route is the core struct of the application.
// It represents a route specifying its path, which vehicle may use it and sets of
// useful notes to describe the path.
// Optionally, it can be specified a Name, Description, CoverPhotoURL and a
// StartCity and FinishCity name
// A route must have:
//   - path
//   - at least one vehicle
//   - at least one note set
//   - author id, null only if the user is deleted
type Route struct {
	ID            uuid.UUID
	Name          string
	Description   string
	CoverPhotoURL string
	Path          LineString
	LengthM       float64 // metres, computed by PostGIS from Path — read-only
	StartCity     string
	FinishCity    string
	Vehicles      []Vehicle
	Waypoints     []Waypoint
	NoteSets      []NoteSet
	AuthorID      uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Validate checks that the route values are accepted values.
// It gathers eventual errors with `errors.Join`.
// Returns nil if no errors are found.
func (r Route) Validate() error {
	var errs []error

	if !r.Path.Valid() {
		errs = append(errs, invalid("path", "at least two valid coordinates"))
	}
	if len(r.Vehicles) == 0 {
		errs = append(errs, invalid("vehicles", "specify at least one valid vehicle"))
	}
	for i, v := range r.Vehicles {
		if !v.Valid() {
			errs = append(errs, invalid(fmt.Sprintf("vehicles[%d]", i), "vehicle not valid: "+string(v)))
		}
	}
	for i, wp := range r.Waypoints {
		if err := wp.Validate(); err != nil {
			errs = append(errs, wrapIndex("waypoints", i, err))
		}
	}
	// NOTE: "must have at least one note set" is a creation-time rule
	// enforced at the HTTP boundary (POST /v1/routes requires note_set in
	// the body). It is NOT a structural invariant of the entity — a route
	// loaded from the DB has empty NoteSets until the notes service is
	// queried, and that state must remain valid.
	for i, ns := range r.NoteSets {
		if err := ns.Validate(); err != nil {
			errs = append(errs, wrapIndex("noteSets", i, err))
		}
	}

	return errors.Join(errs...)
}
