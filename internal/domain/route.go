package domain

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Field caps for a Route. They mirror the DB constraints in migration
// 00001_base_schema.sql.
const (
	routeNameMaxLen = 120
	routeDescMaxLen = 1000
	routeCityMaxLen = 120
	// routePathMaxPoints caps the number of vertices of a path. At ~53 pts/km a
	// 30 km route is ~1 600 points, so 5 000 leaves ample headroom for denser or
	// longer routes while stopping a payload from carrying millions of points.
	routePathMaxPoints = 5_000
	// maxVehicles is the number of distinct vehicle types — a route cannot list
	// more than this, and it cannot repeat one.
	maxVehicles = 4
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
// Optionally, it can be specified a Name, Description and a
// StartCity and FinishCity name
// A route must have:
//   - path
//   - at least one vehicle
//   - at least one note set
//   - author id, null only if the user is deleted
type Route struct {
	ID          uuid.UUID
	Name        string
	Description string
	Path        LineString
	LengthM     float64 // metres, computed by PostGIS from Path — read-only
	StartCity   string
	FinishCity  string
	Vehicles    []Vehicle
	Waypoints   []Waypoint
	NoteSets    []NoteSet
	AuthorID    uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Validate checks that the route values are accepted values.
// It gathers eventual errors with `errors.Join`.
// Returns nil if no errors are found.
func (r Route) Validate() error {
	var errs []error

	if !r.Path.Valid() {
		errs = append(errs, invalid("path", "at least two valid coordinates"))
	}
	if len(r.Path) > routePathMaxPoints {
		errs = append(errs, invalid("path", "max %d points", routePathMaxPoints))
	}
	if utf8.RuneCountInString(r.Name) > routeNameMaxLen {
		errs = append(errs, invalid("name", "max %d characters", routeNameMaxLen))
	}
	if utf8.RuneCountInString(r.Description) > routeDescMaxLen {
		errs = append(errs, invalid("description", "max %d characters", routeDescMaxLen))
	}
	if utf8.RuneCountInString(r.StartCity) > routeCityMaxLen {
		errs = append(errs, invalid("startCity", "max %d characters", routeCityMaxLen))
	}
	if utf8.RuneCountInString(r.FinishCity) > routeCityMaxLen {
		errs = append(errs, invalid("finishCity", "max %d characters", routeCityMaxLen))
	}
	if len(r.Vehicles) == 0 {
		errs = append(errs, invalid("vehicles", "specify at least one valid vehicle"))
	}
	if len(r.Vehicles) > maxVehicles {
		errs = append(errs, invalid("vehicles", "at most %d vehicles allowed", maxVehicles))
	}
	seen := make(map[Vehicle]struct{}, len(r.Vehicles))
	for i, v := range r.Vehicles {
		if !v.Valid() {
			errs = append(errs, invalid(fmt.Sprintf("vehicles[%d]", i), "vehicle not valid: "+string(v)))
		}
		if _, dup := seen[v]; dup {
			errs = append(errs, invalid(fmt.Sprintf("vehicles[%d]", i), "duplicate vehicle: "+string(v)))
		}
		seen[v] = struct{}{}
	}
	for i, wp := range r.Waypoints {
		if err := wp.Validate(); err != nil {
			errs = append(errs, wrapIndex("waypoints", i, err))
		}
	}
	for i, ns := range r.NoteSets {
		if err := ns.Validate(); err != nil {
			errs = append(errs, wrapIndex("noteSets", i, err))
		}
	}

	return errors.Join(errs...)
}
