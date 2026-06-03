package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
)

// NoteSetInput is the writable portion of a NoteSet.
type NoteSetInput struct {
	Name  string   `json:"name"`
	Notes []NoteIn `json:"notes"`
}

// ToDomain maps to domain.NoteSet. The caller fills in RouteID and AuthorID
// before validation.
func (in NoteSetInput) ToDomain() domain.NoteSet {
	notes := make([]domain.Note, len(in.Notes))
	for i, n := range in.Notes {
		notes[i] = n.ToDomain()
	}
	return domain.NoteSet{Name: in.Name, Notes: notes}
}

// NotesInput is the payload for appending several notes to an existing set in a
// single request. Wrapped in an object (rather than a bare array) so the shape
// can grow without breaking clients.
type NotesInput struct {
	Notes []NoteIn `json:"notes"`
}

// ToDomain maps the batch to domain notes, preserving order.
func (in NotesInput) ToDomain() []domain.Note {
	notes := make([]domain.Note, len(in.Notes))
	for i, n := range in.Notes {
		notes[i] = n.ToDomain()
	}
	return notes
}

// NoteIn is the writable portion of a Note. Severity is a pointer because
// the column is nullable; absent means "no severity" rather than zero.
type NoteIn struct {
	Position    Coordinate `json:"position"`
	Order       int        `json:"order"`
	Type        string     `json:"type"`
	Severity    *int       `json:"severity,omitempty"`
	Direction   string     `json:"direction,omitempty"`
	Description string     `json:"description,omitempty"`
}

// ToDomain maps to domain.Note.
func (n NoteIn) ToDomain() domain.Note {
	return domain.Note{
		Position:    n.Position.ToPoint(),
		Order:       n.Order,
		Type:        domain.NoteType(n.Type),
		Severity:    n.Severity,
		Direction:   domain.DirectionType(n.Direction),
		Description: n.Description,
	}
}

// ── Response shapes ────────────────────────────────────────────────────────

// NoteSetResponse is the read shape of a note set with its notes hydrated.
type NoteSetResponse struct {
	ID        uuid.UUID      `json:"id"`
	Name      string         `json:"name,omitempty"`
	RouteID   uuid.UUID      `json:"route_id"`
	AuthorID  uuid.UUID      `json:"author_id,omitempty"`
	Notes     []NoteResponse `json:"notes"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// FromNoteSet maps a domain note set.
func FromNoteSet(ns domain.NoteSet) NoteSetResponse {
	return NoteSetResponse{
		ID:        ns.ID,
		Name:      ns.Name,
		RouteID:   ns.RouteID,
		AuthorID:  ns.AuthorID,
		Notes:     FromNotes(ns.Notes),
		CreatedAt: ns.CreatedAt,
		UpdatedAt: ns.UpdatedAt,
	}
}

// FromNoteSets is the slice helper.
func FromNoteSets(nss []domain.NoteSet) []NoteSetResponse {
	out := make([]NoteSetResponse, len(nss))
	for i, ns := range nss {
		out[i] = FromNoteSet(ns)
	}
	return out
}

// NoteResponse is the read shape of a note.
type NoteResponse struct {
	ID          uuid.UUID  `json:"id"`
	Position    Coordinate `json:"position"`
	Order       int        `json:"order"`
	Type        string     `json:"type"`
	Severity    *int       `json:"severity,omitempty"`
	Direction   string     `json:"direction,omitempty"`
	Description string     `json:"description,omitempty"`
}

// FromNote maps a domain note.
func FromNote(n domain.Note) NoteResponse {
	return NoteResponse{
		ID:          n.ID,
		Position:    CoordFromPoint(n.Position),
		Order:       n.Order,
		Type:        string(n.Type),
		Severity:    n.Severity,
		Direction:   string(n.Direction),
		Description: n.Description,
	}
}

// FromNotes is the slice helper.
func FromNotes(ns []domain.Note) []NoteResponse {
	out := make([]NoteResponse, len(ns))
	for i, n := range ns {
		out[i] = FromNote(n)
	}
	return out
}
