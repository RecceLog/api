package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
)

// NoteService is the use-case layer for note sets and notes. It enforces that
// only the note set's author may mutate its contents.
type NoteService struct {
	notes NoteAggregate
}

// NewNoteService wires the note use cases to the note aggregate service.
func NewNoteService(notesSvc NoteAggregate) *NoteService {
	return &NoteService{notes: notesSvc}
}

// GetNotes returns the notes of a set. Returns domain.ErrNotFound if the set
// itself does not exist (distinct from an existing but empty set).
func (s *NoteService) GetNotes(ctx context.Context, setID uuid.UUID) ([]domain.Note, error) {
	return s.notes.ListBySetID(ctx, setID)
}

// AddNotes appends one or more notes to a set the caller authored, atomically:
// the aggregate validates every note and inserts the whole batch in one
// transaction via unnest, so a single invalid or failing note prevents the
// others from being persisted. Only the set's creator may add notes.
func (s *NoteService) AddNotes(ctx context.Context, callerID, setID uuid.UUID, notes []domain.Note) ([]domain.Note, error) {
	if err := s.authorizeSetOwner(ctx, callerID, setID); err != nil {
		return nil, err
	}
	return s.notes.AddNotes(ctx, setID, notes)
}

// UpdateNote replaces a note in a set the caller authored.
func (s *NoteService) UpdateNote(ctx context.Context, callerID, setID uuid.UUID, note domain.Note) (domain.Note, error) {
	if err := s.authorizeSetOwner(ctx, callerID, setID); err != nil {
		return domain.Note{}, err
	}
	return s.notes.UpdateNote(ctx, setID, note)
}

// DeleteNoteSet deletes a set the caller authored; CASCADE removes its notes.
func (s *NoteService) DeleteNoteSet(ctx context.Context, callerID, setID uuid.UUID) error {
	if err := s.authorizeSetOwner(ctx, callerID, setID); err != nil {
		return err
	}
	return s.notes.DeleteNoteSet(ctx, setID)
}

// DeleteNote deletes a note from a set the caller authored.
func (s *NoteService) DeleteNote(ctx context.Context, callerID, setID, noteID uuid.UUID) error {
	if err := s.authorizeSetOwner(ctx, callerID, setID); err != nil {
		return err
	}
	return s.notes.DeleteNote(ctx, setID, noteID)
}

// authorizeSetOwner returns nil only when callerID is the note set's author;
// domain.ErrNotFound when the set is absent, domain.ErrForbidden otherwise.
func (s *NoteService) authorizeSetOwner(ctx context.Context, callerID, setID uuid.UUID) error {
	author, err := s.notes.GetSetAuthor(ctx, setID)
	return authorizeOwner(callerID, author, err)
}
