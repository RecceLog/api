package notes

import (
	"context"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
)

// Service owns the application logic for the NoteSet aggregate (note_sets +
// notes). It does NOT know about routes — the route_id on NoteSet is just a
// foreign key the caller fills in.
type Service struct {
	repo Repository
	tx   Transactor
}

// NewService wires a Service to its Repository and Transactor.
// The Transactor guarantees that intra-aggregate writes (note_set row + its
// notes) are atomic regardless of whether the caller has already opened
// an outer transaction — InTx is reentrant.
func NewService(repo Repository, tx Transactor) *Service {
	return &Service{repo: repo, tx: tx}
}

// CreateNoteSet validates the note set, inserts the note_set row, and
// batch-inserts its notes inside a single transaction. If the surrounding
// ctx already carries a tx (because the HTTP handler is composing route +
// note set), InTx joins it instead of starting a new one.
func (s *Service) CreateNoteSet(ctx context.Context, ns domain.NoteSet) (domain.NoteSet, error) {
	if err := ns.Validate(); err != nil {
		return domain.NoteSet{}, err
	}

	var saved domain.NoteSet
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		saved, err = s.repo.CreateNoteSet(ctx, ns)
		if err != nil {
			return err
		}
		notes, err := s.repo.AddNotes(ctx, saved.ID, ns.Notes)
		if err != nil {
			return err
		}
		saved.Notes = notes
		return nil
	})
	if err != nil {
		return domain.NoteSet{}, err
	}
	return saved, nil
}

// ListByRouteID returns every note set of a route with its notes pre-loaded.
func (s *Service) ListByRouteID(ctx context.Context, routeID uuid.UUID) ([]domain.NoteSet, error) {
	return s.repo.ListByRouteID(ctx, routeID)
}

// ListBySetID returns the notes of a specific note set.
// Returns domain.ErrNotFound if the set does not exist.
func (s *Service) ListBySetID(ctx context.Context, setID uuid.UUID) ([]domain.Note, error) {
	return s.repo.ListBySetID(ctx, setID)
}

// GetSetAuthor returns a note set's author id (uuid.Nil if unset). Used by the
// HTTP layer to authorize mutations of the set and its notes.
func (s *Service) GetSetAuthor(ctx context.Context, setID uuid.UUID) (uuid.UUID, error) {
	return s.repo.GetSetAuthor(ctx, setID)
}

// UpdateNote replaces a single note's mutable fields. Validates the note
// against domain invariants before touching the database. The setID is the
// parent set the note must belong to — mismatches surface as ErrNotFound.
func (s *Service) UpdateNote(ctx context.Context, setID uuid.UUID, n domain.Note) (domain.Note, error) {
	if err := n.Validate(); err != nil {
		return domain.Note{}, err
	}
	return s.repo.UpdateNote(ctx, setID, n)
}

// DeleteNoteSet removes a note set. CASCADE reaps its notes.
func (s *Service) DeleteNoteSet(ctx context.Context, setID uuid.UUID) error {
	return s.repo.DeleteNoteSet(ctx, setID)
}

// DeleteNote removes a single note. The setID guards against deleting a note
// through the wrong parent URL — mismatches surface as ErrNotFound rather than
// silently deleting the wrong row.
func (s *Service) DeleteNote(ctx context.Context, setID, noteID uuid.UUID) error {
	return s.repo.DeleteNote(ctx, setID, noteID)
}
