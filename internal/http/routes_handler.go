package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/auth"
	"github.com/RecceLog/api/internal/domain"
	"github.com/RecceLog/api/internal/http/dto"
	"github.com/RecceLog/api/internal/http/problem"
	routesdto "github.com/RecceLog/api/internal/routes/dto"
)

// handleCreateRoute persists a route together with an initial note set
// inside a single transaction opened here at the boundary. If anything fails
// between the route insert and the note_set insert, both roll back.
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
	if err := decodeJSON(r, &body); err != nil {
		problem.BadRequest(w, r, err.Error())
		return
	}
	if body.NoteSet == nil {
		problem.From(w, r, &domain.ValidationError{
			Field:   "note_set",
			Message: "must include at least one note set when creating a route",
		})
		return
	}

	// protected route: the auth middleware guarantees a user in context.
	user, _ := auth.UserFrom(r.Context())

	route := body.Route.ToDomain()
	route.AuthorID = user.ID
	ns := body.NoteSet.ToDomain()
	ns.AuthorID = user.ID
	noteSet := &ns

	var (
		savedRoute   domain.Route
		savedNoteSet domain.NoteSet
	)
	err := s.tx.InTx(r.Context(), func(ctx context.Context) error {
		var err error
		savedRoute, err = s.routes.Create(ctx, route)
		if err != nil {
			return err
		}
		if noteSet != nil {
			noteSet.RouteID = savedRoute.ID
			savedNoteSet, err = s.notes.CreateNoteSet(ctx, *noteSet)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		problem.From(w, r, err)
		return
	}

	var noteSets []domain.NoteSet
	if noteSet != nil {
		noteSets = []domain.NoteSet{savedNoteSet}
	}
	writeJSON(w, http.StatusCreated, dto.FromRouteDetail(savedRoute, noteSets))
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
// (notes pre-loaded). The handler composes the two services — each one stays
// aggregate-scoped.
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

	route, err := s.routes.GetDetailsByID(r.Context(), id)
	if err != nil {
		problem.From(w, r, err)
		return
	}
	noteSets, err := s.notes.ListByRouteID(r.Context(), id)
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

	var summaries []routesdto.RouteSummary
	if len(vehicles) > 0 {
		summaries, err = s.routes.ListNearbyByVehicles(r.Context(), center, radiusM, vehicles)
	} else {
		summaries, err = s.routes.ListNearby(r.Context(), center, radiusM)
	}
	if err != nil {
		problem.From(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.FromSummaries(summaries))
}

// parseVehicles maps the raw `vehicle` query values to domain vehicles. It does
// not validate membership — the service rejects unknown values so the handler
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
// Single-aggregate write — the notes service's own Transactor handles
// atomicity between note_set + notes.
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
	if err := decodeJSON(r, &body); err != nil {
		problem.BadRequest(w, r, err.Error())
		return
	}

	// protected route: the auth middleware guarantees a user in context.
	user, _ := auth.UserFrom(r.Context())

	ns := body.ToDomain()
	ns.RouteID = routeID
	ns.AuthorID = user.ID

	saved, err := s.notes.CreateNoteSet(r.Context(), ns)
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

	author, err := s.routes.GetAuthor(r.Context(), routeID)
	if !requireOwner(w, r, author, err) {
		return
	}

	var body dto.WaypointIn
	if err := decodeJSON(r, &body); err != nil {
		problem.BadRequest(w, r, err.Error())
		return
	}

	saved, err := s.routes.AddWaypoint(r.Context(), routeID, body.ToDomain())
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

	author, err := s.routes.GetAuthor(r.Context(), routeID)
	if !requireOwner(w, r, author, err) {
		return
	}

	var body dto.WaypointIn
	if err := decodeJSON(r, &body); err != nil {
		problem.BadRequest(w, r, err.Error())
		return
	}

	wp := body.ToDomain()
	wp.ID = waypointID

	saved, err := s.routes.UpdateWaypoint(r.Context(), routeID, wp)
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

	author, err := s.routes.GetAuthor(r.Context(), routeID)
	if !requireOwner(w, r, author, err) {
		return
	}

	if err := s.routes.DeleteWaypoint(r.Context(), routeID, waypointID); err != nil {
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

	author, err := s.routes.GetAuthor(r.Context(), routeID)
	if !requireOwner(w, r, author, err) {
		return
	}

	if err := s.routes.Delete(r.Context(), routeID); err != nil {
		problem.From(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requireOwner authorizes a mutation: the authenticated caller must be the
// author of the resource. lookupErr is the result of fetching the resource's
// author (e.g. domain.ErrNotFound when it is absent). It writes the matching
// problem (404 when the resource is missing, 403 when the caller is not the
// author) and returns false when the handler must stop. A NULL author
// (uuid.Nil, e.g. after the author was deleted) is owned by nobody → 403.
func requireOwner(w http.ResponseWriter, r *http.Request, author uuid.UUID, lookupErr error) bool {
	if lookupErr != nil {
		problem.From(w, r, lookupErr)
		return false
	}
	user, ok := auth.UserFrom(r.Context())
	if !ok || author == uuid.Nil || author != user.ID {
		problem.From(w, r, domain.ErrForbidden)
		return false
	}
	return true
}

// decodeJSON applies the standard guards: reject unknown fields so clients
// can't silently send mis-named keys.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
