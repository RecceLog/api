package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
	routesdto "github.com/RecceLog/api/internal/routes/dto"
)

// The interfaces below are declared on the consumer side (the app layer) so the
// use-case services depend on behaviour, not on the concrete aggregate
// services. The concrete routes/notes/users services satisfy them; tests can
// substitute small fakes.

// RouteAggregate is the slice of the route aggregate service the app layer
// uses. EnrichCities mutates the route in place to fill start/finish city
// names and must be called outside any transaction.
type RouteAggregate interface {
	Create(ctx context.Context, r domain.Route) (domain.Route, error)
	GetDetailsByID(ctx context.Context, id uuid.UUID) (domain.Route, error)
	List(ctx context.Context) ([]routesdto.RouteSummary, error)
	GetAuthor(ctx context.Context, id uuid.UUID) (uuid.UUID, error)
	ListNearbyByVehicles(ctx context.Context, center domain.Point, radiusM float64, vehicles []domain.Vehicle) ([]routesdto.RouteSummary, error)
	AddWaypoint(ctx context.Context, routeID uuid.UUID, w domain.Waypoint) (domain.Waypoint, error)
	UpdateWaypoint(ctx context.Context, routeID uuid.UUID, w domain.Waypoint) (domain.Waypoint, error)
	DeleteWaypoint(ctx context.Context, routeID, waypointID uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
	EnrichCities(ctx context.Context, r *domain.Route)
}

// NoteAggregate is the slice of the note aggregate service the app layer uses.
type NoteAggregate interface {
	CreateNoteSet(ctx context.Context, ns domain.NoteSet) (domain.NoteSet, error)
	ListByRouteID(ctx context.Context, routeID uuid.UUID) ([]domain.NoteSet, error)
	ListBySetID(ctx context.Context, setID uuid.UUID) ([]domain.Note, error)
	GetSetAuthor(ctx context.Context, setID uuid.UUID) (uuid.UUID, error)
	AddNotes(ctx context.Context, setID uuid.UUID, ns []domain.Note) ([]domain.Note, error)
	UpdateNote(ctx context.Context, setID uuid.UUID, n domain.Note) (domain.Note, error)
	DeleteNoteSet(ctx context.Context, setID uuid.UUID) error
	DeleteNote(ctx context.Context, setID, noteID uuid.UUID) error
}

// UserAggregate is the slice of the user aggregate service the app layer uses.
type UserAggregate interface {
	GetByID(ctx context.Context, id uuid.UUID) (domain.User, error)
	ProfilePicture(ctx context.Context, id uuid.UUID) (name, contentType string, err error)
}
