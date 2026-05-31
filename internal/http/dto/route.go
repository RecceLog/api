package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
	routesdto "github.com/RecceLog/api/internal/routes/dto"
)

// Coordinate is a {lat, lng} pair as wire-shape — kept in this order to match
// the convention used by the previous demo client. Internally we always work
// with domain.Point which stores lng first.
type Coordinate struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// ToPoint converts to the domain shape.
func (c Coordinate) ToPoint() domain.Point { return domain.Point{Lng: c.Lng, Lat: c.Lat} }

// CoordFromPoint is the inverse mapping.
func CoordFromPoint(p domain.Point) Coordinate { return Coordinate{Lat: p.Lat, Lng: p.Lng} }

// ── Request bodies ─────────────────────────────────────────────────────────

// CreateRouteRequest is the body of POST /v1/routes.
// The note set is optional but the example client always sends one — this is
// the moment that triggers the cross-aggregate transaction.
type CreateRouteRequest struct {
	Route   RouteInput    `json:"route"`
	NoteSet *NoteSetInput `json:"note_set,omitempty"`
}

// RouteInput is the writable portion of a Route. Path is required and arrives
// as an explicit polyline — the server does no routing.
type RouteInput struct {
	Name          string       `json:"name"`
	Description   string       `json:"description"`
	CoverPhotoURL string       `json:"cover_photo_url"`
	Path          []Coordinate `json:"path"`
	StartCity     string       `json:"start_city"`
	FinishCity    string       `json:"finish_city"`
	Vehicles      []string     `json:"vehicles"`
	Waypoints     []WaypointIn `json:"waypoints"`
}

// ToDomain maps a RouteInput to the corresponding domain.Route value. The
// resulting struct is *not* validated — call .Validate() on it after to
// surface invariant violations.
func (in RouteInput) ToDomain() domain.Route {
	path := make(domain.LineString, len(in.Path))
	for i, c := range in.Path {
		path[i] = c.ToPoint()
	}
	vehicles := make([]domain.Vehicle, len(in.Vehicles))
	for i, v := range in.Vehicles {
		vehicles[i] = domain.Vehicle(v)
	}
	wps := make([]domain.Waypoint, len(in.Waypoints))
	for i, w := range in.Waypoints {
		wps[i] = w.ToDomain()
	}
	return domain.Route{
		Name:          in.Name,
		Description:   in.Description,
		CoverPhotoURL: in.CoverPhotoURL,
		Path:          path,
		StartCity:     in.StartCity,
		FinishCity:    in.FinishCity,
		Vehicles:      vehicles,
		Waypoints:     wps,
	}
}

// WaypointIn is the writable portion of a Waypoint.
type WaypointIn struct {
	Position Coordinate `json:"position"`
	Order    int        `json:"order"`
	Name     string     `json:"name"`
}

// ToDomain maps to domain.Waypoint.
func (w WaypointIn) ToDomain() domain.Waypoint {
	return domain.Waypoint{Position: w.Position.ToPoint(), Order: w.Order, Name: w.Name}
}

// ── Response bodies ────────────────────────────────────────────────────────

// RouteSummaryResponse is what list endpoints return per item.
type RouteSummaryResponse struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	CoverPhotoURL string    `json:"cover_photo_url"`
	LengthM       float64   `json:"length_m"`
	StartCity     string    `json:"start_city"`
	FinishCity    string    `json:"finish_city"`
	Vehicles      []string  `json:"vehicles"`
	AuthorID      uuid.UUID `json:"author_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// FromSummary maps from the service-level projection.
func FromSummary(s routesdto.RouteSummary) RouteSummaryResponse {
	vehicles := make([]string, len(s.Vehicles))
	for i, v := range s.Vehicles {
		vehicles[i] = string(v)
	}
	return RouteSummaryResponse{
		ID:            s.ID,
		Name:          s.Name,
		Description:   s.Description,
		CoverPhotoURL: s.CoverPhotoURL,
		LengthM:       s.LengthM,
		StartCity:     s.StartCity,
		FinishCity:    s.FinishCity,
		Vehicles:      vehicles,
		AuthorID:      s.AuthorID,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

// FromSummaries is the slice helper.
func FromSummaries(ss []routesdto.RouteSummary) []RouteSummaryResponse {
	out := make([]RouteSummaryResponse, len(ss))
	for i, s := range ss {
		out[i] = FromSummary(s)
	}
	return out
}

// RouteDetailResponse is the full hydrated route returned by GET /v1/routes/:id.
// NoteSets are composed by the handler from the notes service.
type RouteDetailResponse struct {
	ID            uuid.UUID         `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	CoverPhotoURL string            `json:"cover_photo_url"`
	Path          []Coordinate      `json:"path"`
	LengthM       float64           `json:"length_m"`
	StartCity     string            `json:"start_city"`
	FinishCity    string            `json:"finish_city"`
	Vehicles      []string          `json:"vehicles"`
	Waypoints     []WaypointResp    `json:"waypoints"`
	NoteSets      []NoteSetResponse `json:"note_sets"`
	AuthorID      uuid.UUID         `json:"author_id,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// FromRouteDetail composes the full response from the loaded route + the note
// sets fetched separately. NoteSets argument may be nil.
func FromRouteDetail(r domain.Route, nss []domain.NoteSet) RouteDetailResponse {
	path := make([]Coordinate, len(r.Path))
	for i, p := range r.Path {
		path[i] = CoordFromPoint(p)
	}
	vehicles := make([]string, len(r.Vehicles))
	for i, v := range r.Vehicles {
		vehicles[i] = string(v)
	}
	wps := make([]WaypointResp, len(r.Waypoints))
	for i, w := range r.Waypoints {
		wps[i] = WaypointResp{
			ID:       w.ID,
			Position: CoordFromPoint(w.Position),
			Order:    w.Order,
			Name:     w.Name,
		}
	}
	return RouteDetailResponse{
		ID:            r.ID,
		Name:          r.Name,
		Description:   r.Description,
		CoverPhotoURL: r.CoverPhotoURL,
		Path:          path,
		LengthM:       r.LengthM,
		StartCity:     r.StartCity,
		FinishCity:    r.FinishCity,
		Vehicles:      vehicles,
		Waypoints:     wps,
		NoteSets:      FromNoteSets(nss),
		AuthorID:      r.AuthorID,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

// WaypointResp is the read shape of a waypoint.
type WaypointResp struct {
	ID       uuid.UUID  `json:"id"`
	Position Coordinate `json:"position"`
	Order    int        `json:"order"`
	Name     string     `json:"name,omitempty"`
}
