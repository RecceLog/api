package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
)

// RouteSummary is the light-weight projection used by list endpoints
// (GET /routes, GET /routes/range/:r). It deliberately omits Path, Waypoints
// and NoteSets so the SQL stays a single-table scan and the JSON payload
// stays small.
type RouteSummary struct {
	ID            uuid.UUID
	Name          string
	Description   string
	CoverPhotoURL string
	LengthM       float64
	StartCity     string
	FinishCity    string
	Vehicles      []domain.Vehicle
	AuthorID      uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
