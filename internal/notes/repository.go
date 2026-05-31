package notes

import (
	"context"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
)

// Repository owns the note_sets and notes tables. Operations on individual
// notes never need to know about routes — the link is the route_id column on
// note_sets, which the caller supplies.
type Repository interface {
	// CreateNoteSet inserts the note_set row and returns it with
	// server-assigned fields (ID, CreatedAt, UpdatedAt) populated. Does NOT
	// write notes — callers must invoke AddNotes for the batch.
	CreateNoteSet(ctx context.Context, ns domain.NoteSet) (domain.NoteSet, error)

	// AddNotes inserts a batch of notes for a note set in a single statement
	// (unnest). Returns the persisted notes with assigned IDs.
	AddNotes(ctx context.Context, setID uuid.UUID, notes []domain.Note) ([]domain.Note, error)

	// ListByRouteID returns every note set for a route with its notes
	// pre-loaded. Two queries (note_sets + notes WHERE set_id IN (...)) merged
	// in Go — no N+1 regardless of how many sets the route has.
	ListByRouteID(ctx context.Context, routeID uuid.UUID) ([]domain.NoteSet, error)

	// ListBySetID returns the notes of a single note set.
	// For GET /v1/notes/:setID.
	ListBySetID(ctx context.Context, setID uuid.UUID) ([]domain.Note, error)

	// GetSetAuthor returns a note set's author id (uuid.Nil if the column is
	// NULL). Used by the HTTP layer for ownership checks on the set and its
	// notes. Returns domain.ErrNotFound if the set does not exist.
	GetSetAuthor(ctx context.Context, setID uuid.UUID) (uuid.UUID, error)

	// UpdateNote replaces a single note's mutable fields. The setID is used
	// as an extra WHERE-clause guard so a noteID belonging to a different set
	// cannot be edited via the wrong URL. Returns domain.ErrNotFound when no
	// row matches (either the note or the set/note pairing is wrong).
	UpdateNote(ctx context.Context, setID uuid.UUID, n domain.Note) (domain.Note, error)

	// DeleteNoteSet removes a note set. CASCADE reaps its notes.
	DeleteNoteSet(ctx context.Context, setID uuid.UUID) error

	// DeleteNote removes a single note. The setID is a WHERE-clause guard so a
	// noteID belonging to a different set cannot be deleted via the wrong URL.
	// Returns domain.ErrNotFound when no row matches.
	DeleteNote(ctx context.Context, setID, noteID uuid.UUID) error
}

// Transactor runs a closure inside a single database transaction.
// Implementations must be reentrant: calling InTx with a ctx that already
// carries an active transaction joins it instead of starting a nested one.
//
// Declared here (consumer-owned) so notes has no import dependency on routes
// or on the storage package.
type Transactor interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}
