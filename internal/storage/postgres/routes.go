package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RecceLog/api/internal/domain"
	"github.com/RecceLog/api/internal/routes/dto"
)

// Routes implements routes.Repository against PostgreSQL+PostGIS.
type Routes struct {
	pool *pgxpool.Pool
}

// NewRoutes wires a Routes repository to the pool. The Querier(ctx, pool)
// helper makes each method pick up the ambient tx when InTx is active.
func NewRoutes(pool *pgxpool.Pool) *Routes {
	return &Routes{pool: pool}
}

const createRouteSQL = `
INSERT INTO routes (
    name, description, cover_photo_url, path,
    start_city, finish_city, vehicles, author_id
) VALUES (
    $1, $2, $3, ST_GeomFromText($4, 4326),
    $5, $6, $7::text[]::vehicle_type[], $8
)
RETURNING id, length_m, created_at, updated_at`

// Create inserts the route row only. Waypoints are inserted separately via
// AddWaypoints so the call site controls batching.
func (r *Routes) Create(ctx context.Context, route domain.Route) (domain.Route, error) {
	q := querier(ctx, r.pool)

	vehicles := vehicleStrings(route.Vehicles)
	authorID := nullableUUID(route.AuthorID)

	err := q.QueryRow(ctx, createRouteSQL,
		route.Name, route.Description, route.CoverPhotoURL, lineStringWKT(route.Path),
		route.StartCity, route.FinishCity, vehicles, authorID,
	).Scan(&route.ID, &route.LengthM, &route.CreatedAt, &route.UpdatedAt)
	if err != nil {
		return domain.Route{}, fmt.Errorf("insert route: %w", err)
	}
	return route, nil
}

const addWaypointsSQL = `
INSERT INTO waypoints (id, route_id, position, "order", name)
SELECT t.id, $1,
       ST_SetSRID(ST_MakePoint(t.lng, t.lat), 4326),
       t.ord, NULLIF(t.name, '')
FROM unnest(
    $2::uuid[], $3::float8[], $4::float8[], $5::int[], $6::text[]
) AS t(id, lng, lat, ord, name)`

// AddWaypoints batches all rows into a single INSERT via unnest. IDs are
// generated client-side (uuid v7) so the returned slice preserves input
// order without relying on RETURNING ordering.
func (r *Routes) AddWaypoints(ctx context.Context, routeID uuid.UUID, wps []domain.Waypoint) ([]domain.Waypoint, error) {
	if len(wps) == 0 {
		return nil, nil
	}
	q := querier(ctx, r.pool)

	ids := make([]uuid.UUID, len(wps))
	lngs := make([]float64, len(wps))
	lats := make([]float64, len(wps))
	orders := make([]int32, len(wps))
	names := make([]string, len(wps))
	for i, w := range wps {
		if w.ID == uuid.Nil {
			id, err := uuid.NewV7()
			if err != nil {
				return nil, fmt.Errorf("gen waypoint uuid: %w", err)
			}
			w.ID = id
		}
		ids[i] = w.ID
		lngs[i] = w.Position.Lng
		lats[i] = w.Position.Lat
		orders[i] = int32(w.Order)
		names[i] = w.Name
		wps[i] = w
	}

	if _, err := q.Exec(ctx, addWaypointsSQL, routeID, ids, lngs, lats, orders, names); err != nil {
		return nil, fmt.Errorf("insert waypoints: %w", err)
	}
	return wps, nil
}

const addWaypointSQL = `
INSERT INTO waypoints (id, route_id, position, "order", name)
VALUES ($1, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326), $5, NULLIF($6, ''))
RETURNING id`

// AddWaypoint inserts a single waypoint. An FK violation on route_id is
// translated to domain.ErrNotFound so callers can return 404.
func (r *Routes) AddWaypoint(ctx context.Context, routeID uuid.UUID, w domain.Waypoint) (domain.Waypoint, error) {
	q := querier(ctx, r.pool)

	if w.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return domain.Waypoint{}, fmt.Errorf("gen waypoint uuid: %w", err)
		}
		w.ID = id
	}

	var returnedID uuid.UUID
	err := q.QueryRow(ctx, addWaypointSQL,
		w.ID, routeID,
		w.Position.Lng, w.Position.Lat,
		int32(w.Order), w.Name,
	).Scan(&returnedID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation
			return domain.Waypoint{}, domain.ErrNotFound
		}
		return domain.Waypoint{}, fmt.Errorf("insert waypoint: %w", err)
	}
	return w, nil
}

const updateWaypointSQL = `
UPDATE waypoints SET
    position = ST_SetSRID(ST_MakePoint($3, $4), 4326),
    "order"  = $5,
    name     = NULLIF($6, '')
WHERE id = $1 AND route_id = $2
RETURNING id`

// UpdateWaypoint replaces the mutable fields of a waypoint. The route_id
// guard prevents editing via the wrong parent URL.
func (r *Routes) UpdateWaypoint(ctx context.Context, routeID uuid.UUID, w domain.Waypoint) (domain.Waypoint, error) {
	q := querier(ctx, r.pool)

	var returnedID uuid.UUID
	err := q.QueryRow(ctx, updateWaypointSQL,
		w.ID, routeID,
		w.Position.Lng, w.Position.Lat,
		int32(w.Order), w.Name,
	).Scan(&returnedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Waypoint{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Waypoint{}, fmt.Errorf("update waypoint: %w", err)
	}
	return w, nil
}

const deleteWaypointSQL = `DELETE FROM waypoints WHERE id = $2 AND route_id = $1`

// DeleteWaypoint removes a single waypoint. The route_id guard prevents
// deleting via the wrong parent URL; a missing row surfaces as ErrNotFound.
func (r *Routes) DeleteWaypoint(ctx context.Context, routeID, waypointID uuid.UUID) error {
	q := querier(ctx, r.pool)
	tag, err := q.Exec(ctx, deleteWaypointSQL, routeID, waypointID)
	if err != nil {
		return fmt.Errorf("delete waypoint: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

const deleteRouteSQL = `DELETE FROM routes WHERE id = $1`

// Delete removes the route row. ON DELETE CASCADE reaps waypoints, note sets
// and notes.
func (r *Routes) Delete(ctx context.Context, id uuid.UUID) error {
	q := querier(ctx, r.pool)
	tag, err := q.Exec(ctx, deleteRouteSQL, id)
	if err != nil {
		return fmt.Errorf("delete route: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

const getRouteSQL = `
SELECT id, name, description, cover_photo_url,
       ST_AsGeoJSON(path)::text,
       length_m, start_city, finish_city, vehicles, author_id,
       created_at, updated_at
FROM routes
WHERE id = $1`

const listWaypointsSQL = `
SELECT id, ST_X(position)::float8, ST_Y(position)::float8, "order", COALESCE(name, '')
FROM waypoints
WHERE route_id = $1
ORDER BY "order" ASC`

// GetDetailsByID returns the route with its waypoints loaded. Note sets are
// NOT loaded here — callers compose them via the notes service.
func (r *Routes) GetDetailsByID(ctx context.Context, id uuid.UUID) (domain.Route, error) {
	q := querier(ctx, r.pool)

	var (
		out         domain.Route
		pathGeoJSON string
		vehicles    []string
		authorID    pgtype.UUID
	)
	err := q.QueryRow(ctx, getRouteSQL, id).Scan(
		&out.ID, &out.Name, &out.Description, &out.CoverPhotoURL,
		&pathGeoJSON,
		&out.LengthM, &out.StartCity, &out.FinishCity, &vehicles, &authorID,
		&out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Route{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Route{}, fmt.Errorf("select route: %w", err)
	}

	out.Path, err = lineStringFromGeoJSON(pathGeoJSON)
	if err != nil {
		return domain.Route{}, fmt.Errorf("decode path: %w", err)
	}
	out.Vehicles = toVehicles(vehicles)
	if authorID.Valid {
		out.AuthorID = uuid.UUID(authorID.Bytes)
	}

	wps, err := r.listWaypoints(ctx, q, out.ID)
	if err != nil {
		return domain.Route{}, err
	}
	out.Waypoints = wps
	return out, nil
}

const getRouteAuthorSQL = `SELECT author_id FROM routes WHERE id = $1`

// GetAuthor returns the route's author id, or uuid.Nil when NULL. ErrNotFound
// when the route is absent.
func (r *Routes) GetAuthor(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	q := querier(ctx, r.pool)

	var authorID pgtype.UUID
	err := q.QueryRow(ctx, getRouteAuthorSQL, id).Scan(&authorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("select route author: %w", err)
	}
	if !authorID.Valid {
		return uuid.Nil, nil
	}
	return uuid.UUID(authorID.Bytes), nil
}

func (r *Routes) listWaypoints(ctx context.Context, q Querier, routeID uuid.UUID) ([]domain.Waypoint, error) {
	rows, err := q.Query(ctx, listWaypointsSQL, routeID)
	if err != nil {
		return nil, fmt.Errorf("select waypoints: %w", err)
	}
	defer rows.Close()

	var out []domain.Waypoint
	for rows.Next() {
		var w domain.Waypoint
		var order int32
		if err := rows.Scan(&w.ID, &w.Position.Lng, &w.Position.Lat, &order, &w.Name); err != nil {
			return nil, fmt.Errorf("scan waypoint: %w", err)
		}
		w.Order = int(order)
		out = append(out, w)
	}
	return out, rows.Err()
}

const listSummariesSQL = `
SELECT id, COALESCE(name, ''), COALESCE(description, ''), COALESCE(cover_photo_url, ''),
       length_m, COALESCE(start_city, ''), COALESCE(finish_city, ''),
       vehicles::text[], author_id, created_at, updated_at
FROM routes
ORDER BY created_at DESC`

// List returns every route as a lightweight summary. Single-table scan, no
// joins.
func (r *Routes) List(ctx context.Context) ([]dto.RouteSummary, error) {
	q := querier(ctx, r.pool)
	rows, err := q.Query(ctx, listSummariesSQL)
	if err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}
	defer rows.Close()
	return scanSummaries(rows)
}

const listNearbySummariesSQL = `
SELECT id, COALESCE(name, ''), COALESCE(description, ''), COALESCE(cover_photo_url, ''),
       length_m, COALESCE(start_city, ''), COALESCE(finish_city, ''),
       vehicles::text[], author_id, created_at, updated_at
FROM routes
WHERE ST_DWithin(
    path::geography,
    ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
    $3
)
ORDER BY path::geography <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography ASC`

// ListNearby returns routes whose path is within radiusM meters of center.
// Uses ST_DWithin for the filter and KNN (<->) for ordering by distance
// ascending. Both terms hit the geography GIST index added in migration 00002.
func (r *Routes) ListNearby(ctx context.Context, center domain.Point, radiusM float64) ([]dto.RouteSummary, error) {
	q := querier(ctx, r.pool)
	rows, err := q.Query(ctx, listNearbySummariesSQL, center.Lng, center.Lat, radiusM)
	if err != nil {
		return nil, fmt.Errorf("list nearby routes: %w", err)
	}
	defer rows.Close()
	return scanSummaries(rows)
}

const listNearbyByVehiclesSQL = `
SELECT id, COALESCE(name, ''), COALESCE(description, ''), COALESCE(cover_photo_url, ''),
       length_m, COALESCE(start_city, ''), COALESCE(finish_city, ''),
       vehicles::text[], author_id, created_at, updated_at
FROM routes
WHERE ST_DWithin(
    path::geography,
    ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
    $3
)
AND vehicles && $4::text[]::vehicle_type[]
ORDER BY path::geography <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography ASC`

// ListNearbyByVehicles is ListNearby narrowed to routes traversable by at
// least one of the given vehicles. The `&&` (array overlap) predicate is
// backed by the GIN index on routes.vehicles.
func (r *Routes) ListNearbyByVehicles(ctx context.Context, center domain.Point, radiusM float64, vehicles []domain.Vehicle) ([]dto.RouteSummary, error) {
	q := querier(ctx, r.pool)
	rows, err := q.Query(ctx, listNearbyByVehiclesSQL, center.Lng, center.Lat, radiusM, vehicleStrings(vehicles))
	if err != nil {
		return nil, fmt.Errorf("list nearby routes by vehicles: %w", err)
	}
	defer rows.Close()
	return scanSummaries(rows)
}

func scanSummaries(rows pgx.Rows) ([]dto.RouteSummary, error) {
	var out []dto.RouteSummary
	for rows.Next() {
		var (
			s        dto.RouteSummary
			vehicles []string
			authorID pgtype.UUID
		)
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Description, &s.CoverPhotoURL,
			&s.LengthM, &s.StartCity, &s.FinishCity, &vehicles, &authorID,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan route summary: %w", err)
		}
		s.Vehicles = toVehicles(vehicles)
		if authorID.Valid {
			s.AuthorID = uuid.UUID(authorID.Bytes)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func vehicleStrings(vs []domain.Vehicle) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}

func toVehicles(ss []string) []domain.Vehicle {
	out := make([]domain.Vehicle, len(ss))
	for i, s := range ss {
		out[i] = domain.Vehicle(s)
	}
	return out
}

// nullableUUID converts uuid.Nil to a SQL NULL and any other value to itself.
// Used for nullable foreign-key columns (e.g. routes.author_id).
func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}
