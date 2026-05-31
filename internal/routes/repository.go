package routes

import (
	"context"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
	"github.com/RecceLog/api/internal/routes/dto"
)

// Repository persists and retrieves Routes and their Waypoints — the tables
// owned by the route aggregate. Note sets / notes live in their own package.
//
// The interface is declared here, on the consumer side, so the storage layer
// adapts to it without leaking driver concerns into the service.
type Repository interface {
	// Create inserts the route row and returns it with server-assigned fields
	// (ID, CreatedAt, UpdatedAt) populated. Does NOT write waypoints — callers
	// must invoke AddWaypoints explicitly, so batch sizing is visible at the
	// call site.
	Create(ctx context.Context, r domain.Route) (domain.Route, error)

	// AddWaypoints attaches waypoints to an existing route in a single batched
	// INSERT (unnest). Returns the persisted waypoints with their assigned IDs.
	AddWaypoints(ctx context.Context, routeID uuid.UUID, wps []domain.Waypoint) ([]domain.Waypoint, error)

	// AddWaypoint attaches a single waypoint to an existing route. Useful for
	// ad-hoc additions after the route is already created. Returns
	// domain.ErrNotFound when the route does not exist.
	AddWaypoint(ctx context.Context, routeID uuid.UUID, w domain.Waypoint) (domain.Waypoint, error)

	// UpdateWaypoint replaces the mutable fields of a single waypoint. The
	// routeID is used as a WHERE-clause guard so a waypointID belonging to a
	// different route cannot be edited via the wrong URL. Returns
	// domain.ErrNotFound when no row matches.
	UpdateWaypoint(ctx context.Context, routeID uuid.UUID, w domain.Waypoint) (domain.Waypoint, error)

	// DeleteWaypoint removes a single waypoint from a route. The routeID is a
	// WHERE-clause guard so a waypointID belonging to a different route cannot
	// be deleted via the wrong URL. Returns domain.ErrNotFound when no row
	// matches.
	DeleteWaypoint(ctx context.Context, routeID, waypointID uuid.UUID) error

	// Delete removes the route row. ON DELETE CASCADE in the schema reaps
	// waypoints, note_sets and notes.
	Delete(ctx context.Context, id uuid.UUID) error

	// GetDetailsByID returns the route row with its waypoints. Note sets are
	// NOT loaded here — the HTTP handler composes them via the notes service.
	// Returns domain.ErrNotFound if absent.
	GetDetailsByID(ctx context.Context, id uuid.UUID) (domain.Route, error)

	// GetAuthor returns the route's author id (uuid.Nil if the column is NULL,
	// i.e. the author was deleted). Used by the HTTP layer for ownership checks
	// without loading the whole aggregate. Returns domain.ErrNotFound if the
	// route does not exist.
	GetAuthor(ctx context.Context, id uuid.UUID) (uuid.UUID, error)

	// List returns the lightweight projection (route table only) for the list
	// endpoint. Single-table scan, no joins.
	List(ctx context.Context) ([]dto.RouteSummary, error)

	// ListNearby returns summaries for routes whose path is within `radiusM`
	// meters of `center`. Backed by PostGIS ST_DWithin + GIST index.
	// Results are ordered by distance ascending.
	ListNearby(ctx context.Context, center domain.Point, radiusM float64) ([]dto.RouteSummary, error)

	// ListNearbyByVehicles is ListNearby restricted to routes traversable by at
	// least one of the given vehicles (array overlap, &&). The containment
	// filter is backed by the GIN index on routes.vehicles. An empty slice is
	// treated as "no vehicle filter" by the caller, not here.
	ListNearbyByVehicles(ctx context.Context, center domain.Point, radiusM float64, vehicles []domain.Vehicle) ([]dto.RouteSummary, error)
}

// Transactor runs a closure inside a single database transaction.
// Implementations must be reentrant: calling InTx with a ctx that already
// carries an active transaction joins it instead of starting a nested one.
//
// Declared here so the HTTP handler can depend on routes.Transactor without
// the storage package being on its import path (any package that needs to
// open a tx can declare its own identical interface).
type Transactor interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}
