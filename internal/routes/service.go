package routes

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
	"github.com/RecceLog/api/internal/routes/dto"
)

// Geocoder resolves geographic coordinates to a human-readable place name.
// Declared here (consumer side) so the routes package never imports the
// mapbox package directly — only main wires the concrete implementation.
// A nil Geocoder disables enrichment (useful in tests and when no token is
// configured).
type Geocoder interface {
	ReverseGeocode(ctx context.Context, lat, lng float64) (string, error)
}

// maxNearbyRadiusM caps a ListNearby search to a sane planetary distance.
// Anything beyond this is almost certainly a client bug (forgotten unit
// conversion) and would force a full table scan.
const maxNearbyRadiusM = 500_000 // 500 km

// Service owns the application logic for the Route aggregate (routes +
// waypoints). It does NOT know about note sets — the HTTP handler composes
// them through the note service.
type Service struct {
	repo     Repository
	tx       Transactor
	geocoder Geocoder // nil → enrichment disabled
}

// NewService wires a Service to its Repository, Transactor and an optional
// Geocoder. Pass nil for geocoder to disable start/finish city enrichment
// (e.g. in tests or when no Mapbox token is configured).
func NewService(repo Repository, tx Transactor, geocoder Geocoder) *Service {
	return &Service{repo: repo, tx: tx, geocoder: geocoder}
}

// Create validates the route, persists the route row, and batch-inserts its
// waypoints inside a single transaction. If the surrounding ctx already
// carries a tx (because the HTTP handler is composing a cross-aggregate
// write), InTx joins it instead of starting a new one.
//
// Returns the stored route with server-assigned fields populated.
// On invariant violation returns an error that wraps domain.ErrValidation.
func (s *Service) Create(ctx context.Context, r domain.Route) (domain.Route, error) {
	s.enrichCities(ctx, &r)

	if err := r.Validate(); err != nil {
		return domain.Route{}, err
	}

	var saved domain.Route
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		saved, err = s.repo.Create(ctx, r)
		if err != nil {
			return err
		}
		if len(r.Waypoints) > 0 {
			wps, err := s.repo.AddWaypoints(ctx, saved.ID, r.Waypoints)
			if err != nil {
				return err
			}
			saved.Waypoints = wps
		}
		return nil
	})
	if err != nil {
		return domain.Route{}, err
	}
	return saved, nil
}

// GetDetailsByID fetches a route with its waypoints. Note sets are NOT loaded
// — callers compose them via the notes service.
// Returns domain.ErrNotFound if the route does not exist.
func (s *Service) GetDetailsByID(ctx context.Context, id uuid.UUID) (domain.Route, error) {
	return s.repo.GetDetailsByID(ctx, id)
}

// List returns the light-weight projection for every route.
// Single-query, no joins.
func (s *Service) List(ctx context.Context) ([]dto.RouteSummary, error) {
	return s.repo.List(ctx)
}

// GetAuthor returns the route's author id (uuid.Nil if unset). Used by the HTTP
// layer to authorize mutations of the route and its waypoints.
func (s *Service) GetAuthor(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.repo.GetAuthor(ctx, id)
}

// ListNearby returns route summaries whose path is within `radiusM` meters of
// `center`. Validates inputs before hitting the database.
func (s *Service) ListNearby(ctx context.Context, center domain.Point, radiusM float64) ([]dto.RouteSummary, error) {
	if err := validateNearby(center, radiusM); err != nil {
		return nil, err
	}
	return s.repo.ListNearby(ctx, center, radiusM)
}

// ListNearbyByVehicles is ListNearby restricted to routes traversable by at
// least one of the given vehicles. With no vehicles it falls back to the
// unfiltered ListNearby so the endpoint behaves sensibly either way.
func (s *Service) ListNearbyByVehicles(ctx context.Context, center domain.Point, radiusM float64, vehicles []domain.Vehicle) ([]dto.RouteSummary, error) {
	if err := validateNearby(center, radiusM); err != nil {
		return nil, err
	}
	if len(vehicles) == 0 {
		return s.repo.ListNearby(ctx, center, radiusM)
	}
	for i, v := range vehicles {
		if !v.Valid() {
			return nil, &domain.ValidationError{
				Field:   fmt.Sprintf("vehicles[%d]", i),
				Message: "vehicle not valid: " + string(v),
			}
		}
	}
	return s.repo.ListNearbyByVehicles(ctx, center, radiusM, vehicles)
}

// validateNearby checks the shared spatial-search inputs (center coordinates
// and radius) used by the nearby endpoints.
func validateNearby(center domain.Point, radiusM float64) error {
	if !center.Valid() {
		return &domain.ValidationError{Field: "center", Message: "invalid coordinates"}
	}
	if radiusM <= 0 {
		return &domain.ValidationError{Field: "radiusM", Message: "must be positive"}
	}
	if radiusM > maxNearbyRadiusM {
		return &domain.ValidationError{
			Field:   "radiusM",
			Message: fmt.Sprintf("must be <= %d", maxNearbyRadiusM),
		}
	}
	return nil
}

// AddWaypoint attaches a single waypoint to an existing route. Validates
// the waypoint against domain invariants before touching the database.
// Returns domain.ErrNotFound when the route does not exist.
func (s *Service) AddWaypoint(ctx context.Context, routeID uuid.UUID, w domain.Waypoint) (domain.Waypoint, error) {
	if err := w.Validate(); err != nil {
		return domain.Waypoint{}, err
	}
	return s.repo.AddWaypoint(ctx, routeID, w)
}

// UpdateWaypoint replaces a waypoint's mutable fields. The routeID guards
// against editing a waypoint through the wrong parent URL — mismatches
// surface as ErrNotFound rather than silently editing the wrong row.
func (s *Service) UpdateWaypoint(ctx context.Context, routeID uuid.UUID, w domain.Waypoint) (domain.Waypoint, error) {
	if err := w.Validate(); err != nil {
		return domain.Waypoint{}, err
	}
	return s.repo.UpdateWaypoint(ctx, routeID, w)
}

// DeleteWaypoint removes a single waypoint from a route. The routeID guards
// against deleting a waypoint through the wrong parent URL — mismatches
// surface as ErrNotFound rather than silently deleting the wrong row.
func (s *Service) DeleteWaypoint(ctx context.Context, routeID, waypointID uuid.UUID) error {
	return s.repo.DeleteWaypoint(ctx, routeID, waypointID)
}

// Delete removes the route. The schema cascades to waypoints, note sets and
// notes, so no orchestration is required here.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

// enrichCities fills StartCity / FinishCity from the route's first and last
// path point when the caller left them empty. Both lookups run concurrently;
// errors are silently ignored so a Mapbox outage never blocks route creation.
// No-op when the geocoder is nil or the path has fewer than two points.
func (s *Service) enrichCities(ctx context.Context, r *domain.Route) {
	if s.geocoder == nil || len(r.Path) < 2 {
		return
	}

	type result struct {
		name string
	}

	startCh := make(chan result, 1)
	finishCh := make(chan result, 1)

	if r.StartCity == "" {
		p := r.Path[0]
		go func() {
			name, _ := s.geocoder.ReverseGeocode(ctx, p.Lat, p.Lng)
			startCh <- result{name}
		}()
	} else {
		startCh <- result{r.StartCity}
	}

	if r.FinishCity == "" {
		p := r.Path[len(r.Path)-1]
		go func() {
			name, _ := s.geocoder.ReverseGeocode(ctx, p.Lat, p.Lng)
			finishCh <- result{name}
		}()
	} else {
		finishCh <- result{r.FinishCity}
	}

	if sr := <-startCh; sr.name != "" {
		r.StartCity = sr.name
	}
	if fr := <-finishCh; fr.name != "" {
		r.FinishCity = fr.name
	}
}
