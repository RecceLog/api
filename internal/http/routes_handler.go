package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/auth"
	"github.com/RecceLog/api/internal/domain"
	"github.com/RecceLog/api/internal/http/dto"
	"github.com/RecceLog/api/internal/http/problem"
)

// handleCreateRoute persists a route together with an initial note set. The
// handler only decodes the request, extracts the caller and writes the
// response; the app layer enriches, opens the transaction and composes the two
// aggregates atomically.
//
// @Summary     Create a route with an initial note set
// @Description Atomically inserts the route, its waypoints and the first note set (with its notes). Queries are run in a transaction
//
//	so if waypoints, note set or notes have an error, none of the data is uploaded to the database.
//
// @Tags        routes
// @Accept      json
// @Produce     json
// @Param       body  body      dto.CreateRouteRequest   true  "Route + note set payload"
// @Success     201   {object}  dto.RouteDetailResponse
// @Failure     400   {object}  problem.Problem          "Malformed JSON or unknown fields"
// @Failure     422   {object}  problem.Problem          "Validation failed (missing note_set, bad enum, etc.)"
// @Failure     500   {object}  problem.Problem
// @Router      /v1/routes [post]
func (s *Server) handleCreateRoute(w http.ResponseWriter, r *http.Request) {
	var body dto.CreateRouteRequest
	if err := decodeJSON(w, r, &body); err != nil {
		problem.BadRequest(w, r, err.Error())
		return
	}

	// protected route: the auth middleware guarantees a user in context.
	user, _ := auth.UserFrom(r.Context())

	var noteSet *domain.NoteSet
	if body.NoteSet != nil {
		ns := body.NoteSet.ToDomain()
		noteSet = &ns
	}

	route, noteSets, err := s.routes.Create(r.Context(), user.ID, body.Route.ToDomain(), noteSet)
	if err != nil {
		problem.From(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.FromRouteDetail(route, noteSets))
}

// handleListRoutes returns every route as a lightweight summary.
//
// @Summary     List all routes
// @Description Returns lightweight summaries (no path, no notes) — backed by a single-table scan.
// @Tags        routes
// @Produce     json
// @Success     200  {array}   dto.RouteSummaryResponse
// @Failure     500  {object}  problem.Problem
// @Router      /v1/routes [get]
func (s *Server) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	summaries, err := s.routes.List(r.Context())
	if err != nil {
		problem.From(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.FromSummaries(summaries))
}

// handleGetRoute returns a route with its waypoints and every note set
// (notes pre-loaded). The app layer composes the two aggregates.
//
// @Summary     Get a route with its note sets
// @Description Composes the route (with waypoints) and all its note sets (with notes) in two queries.
// @Tags        routes
// @Produce     json
// @Param       id   path      string                   true  "Route ID (UUID)"
// @Success     200  {object}  dto.RouteDetailResponse
// @Failure     400  {object}  problem.Problem          "Invalid route id"
// @Failure     404  {object}  problem.Problem          "Route not found"
// @Failure     500  {object}  problem.Problem
// @Router      /v1/routes/{id} [get]
func (s *Server) handleGetRoute(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.BadRequest(w, r, "invalid route id")
		return
	}

	route, noteSets, err := s.routes.GetDetail(r.Context(), id)
	if err != nil {
		problem.From(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.FromRouteDetail(route, noteSets))
}

// handleListRoutesInRange returns summaries for routes whose path is within
// the requested radius (meters) of the center supplied via Latitude /
// Longitude request headers. Matches the demo client's contract.
//
// @Summary     List routes within a radius
// @Description Spatial search using PostGIS ST_DWithin + GIST index. Center is read from headers, radius is the path param (meters). Optionally filter by vehicle: pass one or more `vehicle` query params (e.g. ?vehicle=CAR&vehicle=BIKE) to return only routes traversable by at least one of them.
// @Tags        routes
// @Produce     json
// @Param       radius     path      number  true   "Search radius in meters"
// @Param       vehicle    query     []string false  "Filter: routes traversable by at least one of these vehicles" collectionFormat(multi)
// @Param       Latitude   header    number  true   "Center latitude (WGS84)"
// @Param       Longitude  header    number  true   "Center longitude (WGS84)"
// @Success     200  {array}   dto.RouteSummaryResponse
// @Failure     400  {object}  problem.Problem  "Invalid radius or missing headers"
// @Failure     422  {object}  problem.Problem  "Coordinates out of range, radius too large or invalid vehicle"
// @Failure     500  {object}  problem.Problem
// @Router      /v1/routes/range/{radius} [get]
func (s *Server) handleListRoutesInRange(w http.ResponseWriter, r *http.Request) {
	radiusM, err := strconv.ParseFloat(chi.URLParam(r, "radius"), 64)
	if err != nil {
		problem.BadRequest(w, r, "invalid radius")
		return
	}
	lat, err := strconv.ParseFloat(r.Header.Get("Latitude"), 64)
	if err != nil {
		problem.BadRequest(w, r, "missing or invalid Latitude header")
		return
	}
	lng, err := strconv.ParseFloat(r.Header.Get("Longitude"), 64)
	if err != nil {
		problem.BadRequest(w, r, "missing or invalid Longitude header")
		return
	}

	center := domain.Point{Lng: lng, Lat: lat}
	vehicles := parseVehicles(r.URL.Query()["vehicle"])

	summaries, err := s.routes.ListInRange(r.Context(), center, radiusM, vehicles)
	if err != nil {
		problem.From(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.FromSummaries(summaries))
}

// parseVehicles maps the raw `vehicle` query values to domain vehicles. It does
// not validate membership — the app layer rejects unknown values so the handler
// stays a thin mapping layer.
func parseVehicles(raw []string) []domain.Vehicle {
	if len(raw) == 0 {
		return nil
	}
	out := make([]domain.Vehicle, len(raw))
	for i, v := range raw {
		out[i] = domain.Vehicle(v)
	}
	return out
}

// handleAddNoteSet appends a note set (with its notes) to an existing route.
// Any authenticated user may add a note set to any route, so there is no
// ownership check — the app layer attributes the note set to the caller.
//
// @Summary     Append a note set to a route
// @Description Inserts a new note set (and its notes) for the given route in a single transaction.
// @Tags        routes
// @Accept      json
// @Produce     json
// @Param       id    path      string              true  "Route ID (UUID)"
// @Param       body  body      dto.NoteSetInput    true  "Note set payload"
// @Success     201   {object}  dto.NoteSetResponse
// @Failure     400   {object}  problem.Problem     "Invalid route id or malformed JSON"
// @Failure     404   {object}  problem.Problem     "Route not found"
// @Failure     422   {object}  problem.Problem     "Validation failed"
// @Failure     500   {object}  problem.Problem
// @Router      /v1/routes/{id}/notes [post]
func (s *Server) handleAddNoteSet(w http.ResponseWriter, r *http.Request) {
	routeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.BadRequest(w, r, "invalid route id")
		return
	}
	var body dto.NoteSetInput
	if err := decodeJSON(w, r, &body); err != nil {
		problem.BadRequest(w, r, err.Error())
		return
	}

	// protected route: the auth middleware guarantees a user in context.
	user, _ := auth.UserFrom(r.Context())

	saved, err := s.routes.AddNoteSet(r.Context(), user.ID, routeID, body.ToDomain())
	if err != nil {
		problem.From(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.FromNoteSet(saved))
}

// handleAddWaypoint attaches a single waypoint to an existing route.
//
// @Summary     Add a waypoint to a route
// @Description Inserts a single waypoint for the given route. Returns 404 if the route does not exist.
// @Tags        routes
// @Accept      json
// @Produce     json
// @Param       id    path      string              true  "Route ID (UUID)"
// @Param       body  body      dto.WaypointIn      true  "Waypoint payload"
// @Success     201   {object}  dto.WaypointResp
// @Failure     400   {object}  problem.Problem     "Invalid route id or malformed JSON"
// @Failure     403   {object}  problem.Problem     "Not the route author"
// @Failure     404   {object}  problem.Problem     "Route not found"
// @Failure     422   {object}  problem.Problem     "Validation failed"
// @Failure     500   {object}  problem.Problem
// @Router      /v1/routes/{id}/waypoints [post]
func (s *Server) handleAddWaypoint(w http.ResponseWriter, r *http.Request) {
	routeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.BadRequest(w, r, "invalid route id")
		return
	}

	var body dto.WaypointIn
	if err := decodeJSON(w, r, &body); err != nil {
		problem.BadRequest(w, r, err.Error())
		return
	}

	// protected route: the auth middleware guarantees a user in context.
	user, _ := auth.UserFrom(r.Context())

	saved, err := s.routes.AddWaypoint(r.Context(), user.ID, routeID, body.ToDomain())
	if err != nil {
		problem.From(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.WaypointResp{
		ID:       saved.ID,
		Position: dto.CoordFromPoint(saved.Position),
		Order:    saved.Order,
		Name:     saved.Name,
	})
}

// handleUpdateWaypoint replaces a single waypoint's mutable fields. The
// waypoint must belong to the route whose id is in the URL — mismatches
// surface as 404 rather than silently editing the wrong row.
//
// @Summary     Update a waypoint of a route
// @Description Replaces position, order and name of a waypoint belonging to the given route.
// @Tags        routes
// @Accept      json
// @Produce     json
// @Param       id          path      string              true  "Route ID (UUID)"
// @Param       waypointID  path      string              true  "Waypoint ID (UUID)"
// @Param       body        body      dto.WaypointIn      true  "Updated waypoint payload"
// @Success     200         {object}  dto.WaypointResp
// @Failure     400         {object}  problem.Problem     "Invalid IDs or malformed JSON"
// @Failure     403         {object}  problem.Problem     "Not the route author"
// @Failure     404         {object}  problem.Problem     "Waypoint not found (or not in this route)"
// @Failure     422         {object}  problem.Problem     "Validation failed"
// @Failure     500         {object}  problem.Problem
// @Router      /v1/routes/{id}/waypoints/{waypointID} [patch]
func (s *Server) handleUpdateWaypoint(w http.ResponseWriter, r *http.Request) {
	routeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.BadRequest(w, r, "invalid route id")
		return
	}
	waypointID, err := uuid.Parse(chi.URLParam(r, "waypointID"))
	if err != nil {
		problem.BadRequest(w, r, "invalid waypoint id")
		return
	}

	var body dto.WaypointIn
	if err := decodeJSON(w, r, &body); err != nil {
		problem.BadRequest(w, r, err.Error())
		return
	}

	wp := body.ToDomain()
	wp.ID = waypointID

	// protected route: the auth middleware guarantees a user in context.
	user, _ := auth.UserFrom(r.Context())

	saved, err := s.routes.UpdateWaypoint(r.Context(), user.ID, routeID, wp)
	if err != nil {
		problem.From(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.WaypointResp{
		ID:       saved.ID,
		Position: dto.CoordFromPoint(saved.Position),
		Order:    saved.Order,
		Name:     saved.Name,
	})
}

// handleDeleteWaypoint removes a single waypoint from a route. The waypoint
// must belong to the route whose id is in the URL — mismatches surface as 404
// rather than silently deleting the wrong row.
//
// @Summary     Delete a waypoint of a route
// @Description Deletes a waypoint belonging to the given route. Returns 404 if the waypoint does not exist or is not in this route.
// @Tags        routes
// @Param       id          path  string  true  "Route ID (UUID)"
// @Param       waypointID  path  string  true  "Waypoint ID (UUID)"
// @Success     204         "No Content"
// @Failure     400         {object}  problem.Problem  "Invalid IDs"
// @Failure     403         {object}  problem.Problem  "Not the route author"
// @Failure     404         {object}  problem.Problem  "Waypoint not found (or not in this route)"
// @Failure     500         {object}  problem.Problem
// @Router      /v1/routes/{id}/waypoints/{waypointID} [delete]
func (s *Server) handleDeleteWaypoint(w http.ResponseWriter, r *http.Request) {
	routeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.BadRequest(w, r, "invalid route id")
		return
	}
	waypointID, err := uuid.Parse(chi.URLParam(r, "waypointID"))
	if err != nil {
		problem.BadRequest(w, r, "invalid waypoint id")
		return
	}

	// protected route: the auth middleware guarantees a user in context.
	user, _ := auth.UserFrom(r.Context())

	if err := s.routes.DeleteWaypoint(r.Context(), user.ID, routeID, waypointID); err != nil {
		problem.From(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteRoute removes a route. Only the route's author may delete it.
// ON DELETE CASCADE reaps its waypoints, note sets and notes.
//
// @Summary     Delete a route
// @Description Deletes a route owned by the caller; cascades to its waypoints, note sets and notes. Only the route author may delete it.
// @Tags        routes
// @Param       id   path  string  true  "Route ID (UUID)"
// @Success     204  "No Content"
// @Failure     400  {object}  problem.Problem  "Invalid route id"
// @Failure     403  {object}  problem.Problem  "Not the route author"
// @Failure     404  {object}  problem.Problem  "Route not found"
// @Failure     500  {object}  problem.Problem
// @Router      /v1/routes/{id} [delete]
func (s *Server) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	routeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.BadRequest(w, r, "invalid route id")
		return
	}

	// protected route: the auth middleware guarantees a user in context.
	user, _ := auth.UserFrom(r.Context())

	if err := s.routes.Delete(r.Context(), user.ID, routeID); err != nil {
		problem.From(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maxBodyBytes caps the size of a decoded request body. It is generous enough
// for a route with a very long path (thousands of points) yet small enough to
// stop an authenticated client from exhausting memory with a multi-megabyte
// payload.
const maxBodyBytes = 4 << 20 // 4 MiB

// decodeJSON applies the standard guards: it caps the body size with
// http.MaxBytesReader (so an oversized payload is rejected instead of being
// read into memory) and rejects unknown fields so clients can't silently send
// mis-named keys.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
