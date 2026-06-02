package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
	routesdto "github.com/RecceLog/api/internal/routes/dto"
)

// RouteService is the use-case layer for routes. It composes the route and note
// aggregate services, owns the cross-aggregate transaction of route creation,
// and enforces the authorization rules for route mutations.
type RouteService struct {
	routes RouteAggregate
	notes  NoteAggregate
	tx     Transactor
}

// NewRouteService wires the route use cases to the aggregate services and the
// Transactor used for the cross-aggregate create.
func NewRouteService(routesSvc RouteAggregate, notesSvc NoteAggregate, tx Transactor) *RouteService {
	return &RouteService{routes: routesSvc, notes: notesSvc, tx: tx}
}

// Create persists a route together with its initial note set in a single
// transaction, attributing both to callerID. City names are resolved (Mapbox)
// BEFORE the transaction opens, so the external call never holds a pooled DB
// connection. A nil noteSet violates the creation rule "a route is created with
// at least one note set" and yields a validation error.
func (s *RouteService) Create(ctx context.Context, callerID uuid.UUID, route domain.Route, noteSet *domain.NoteSet) (domain.Route, []domain.NoteSet, error) {
	if noteSet == nil {
		return domain.Route{}, nil, &domain.ValidationError{
			Field:   "note_set",
			Message: "must include at least one note set when creating a route",
		}
	}

	route.AuthorID = callerID
	noteSet.AuthorID = callerID

	// External enrichment happens outside the transaction on purpose.
	s.routes.EnrichCities(ctx, &route)

	var (
		savedRoute   domain.Route
		savedNoteSet domain.NoteSet
	)
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		savedRoute, err = s.routes.Create(ctx, route)
		if err != nil {
			return err
		}
		noteSet.RouteID = savedRoute.ID
		savedNoteSet, err = s.notes.CreateNoteSet(ctx, *noteSet)
		return err
	})
	if err != nil {
		return domain.Route{}, nil, err
	}
	return savedRoute, []domain.NoteSet{savedNoteSet}, nil
}

// List returns lightweight summaries of every route.
func (s *RouteService) List(ctx context.Context) ([]routesdto.RouteSummary, error) {
	return s.routes.List(ctx)
}

// GetDetail composes a route (with waypoints) and all its note sets (with
// notes). Returns domain.ErrNotFound if the route does not exist.
func (s *RouteService) GetDetail(ctx context.Context, id uuid.UUID) (domain.Route, []domain.NoteSet, error) {
	route, err := s.routes.GetDetailsByID(ctx, id)
	if err != nil {
		return domain.Route{}, nil, err
	}
	noteSets, err := s.notes.ListByRouteID(ctx, id)
	if err != nil {
		return domain.Route{}, nil, err
	}
	return route, noteSets, nil
}

// ListInRange returns route summaries within radiusM of center, optionally
// restricted to routes traversable by at least one of the given vehicles (an
// empty vehicles slice means no vehicle filter).
func (s *RouteService) ListInRange(ctx context.Context, center domain.Point, radiusM float64, vehicles []domain.Vehicle) ([]routesdto.RouteSummary, error) {
	return s.routes.ListNearbyByVehicles(ctx, center, radiusM, vehicles)
}

// AddNoteSet appends a note set to an existing route. By design ANY
// authenticated user may add a note set to ANY route, so there is deliberately
// no ownership check here — the note set is simply attributed to the caller.
func (s *RouteService) AddNoteSet(ctx context.Context, callerID, routeID uuid.UUID, noteSet domain.NoteSet) (domain.NoteSet, error) {
	noteSet.RouteID = routeID
	noteSet.AuthorID = callerID
	return s.notes.CreateNoteSet(ctx, noteSet)
}

// AddWaypoint adds a waypoint to a route the caller owns.
func (s *RouteService) AddWaypoint(ctx context.Context, callerID, routeID uuid.UUID, wp domain.Waypoint) (domain.Waypoint, error) {
	if err := s.authorizeRouteOwner(ctx, callerID, routeID); err != nil {
		return domain.Waypoint{}, err
	}
	return s.routes.AddWaypoint(ctx, routeID, wp)
}

// UpdateWaypoint replaces a waypoint of a route the caller owns.
func (s *RouteService) UpdateWaypoint(ctx context.Context, callerID, routeID uuid.UUID, wp domain.Waypoint) (domain.Waypoint, error) {
	if err := s.authorizeRouteOwner(ctx, callerID, routeID); err != nil {
		return domain.Waypoint{}, err
	}
	return s.routes.UpdateWaypoint(ctx, routeID, wp)
}

// DeleteWaypoint removes a waypoint from a route the caller owns.
func (s *RouteService) DeleteWaypoint(ctx context.Context, callerID, routeID, waypointID uuid.UUID) error {
	if err := s.authorizeRouteOwner(ctx, callerID, routeID); err != nil {
		return err
	}
	return s.routes.DeleteWaypoint(ctx, routeID, waypointID)
}

// Delete removes a route the caller owns; the schema cascades to its waypoints,
// note sets and notes.
func (s *RouteService) Delete(ctx context.Context, callerID, routeID uuid.UUID) error {
	if err := s.authorizeRouteOwner(ctx, callerID, routeID); err != nil {
		return err
	}
	return s.routes.Delete(ctx, routeID)
}

// authorizeRouteOwner returns nil only when callerID is the route's author;
// domain.ErrNotFound when the route is absent, domain.ErrForbidden otherwise.
func (s *RouteService) authorizeRouteOwner(ctx context.Context, callerID, routeID uuid.UUID) error {
	author, err := s.routes.GetAuthor(ctx, routeID)
	return authorizeOwner(callerID, author, err)
}
