package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/app"
	"github.com/RecceLog/api/internal/domain"
)

// noteMutation is one authorized note-mutating use case, plus a probe that
// reports whether the underlying aggregate method ran.
type noteMutation struct {
	name   string
	invoke func(svc *app.NoteService, caller, setID uuid.UUID) error
	ran    func(f *fakeNoteAgg) bool
}

func noteMutations() []noteMutation {
	return []noteMutation{
		{
			name: "AddNotes",
			invoke: func(svc *app.NoteService, caller, setID uuid.UUID) error {
				_, err := svc.AddNotes(context.Background(), caller, setID, []domain.Note{{
					Position: domain.Point{Lng: 1, Lat: 1}, Type: domain.NoteWarning,
				}})
				return err
			},
			ran: func(f *fakeNoteAgg) bool { return f.addNotesCalled },
		},
		{
			name: "UpdateNote",
			invoke: func(svc *app.NoteService, caller, setID uuid.UUID) error {
				_, err := svc.UpdateNote(context.Background(), caller, setID, domain.Note{
					Position: domain.Point{Lng: 1, Lat: 1}, Type: domain.NoteWarning,
				})
				return err
			},
			ran: func(f *fakeNoteAgg) bool { return f.updateCalled },
		},
		{
			name: "DeleteNoteSet",
			invoke: func(svc *app.NoteService, caller, setID uuid.UUID) error {
				return svc.DeleteNoteSet(context.Background(), caller, setID)
			},
			ran: func(f *fakeNoteAgg) bool { return f.deleteSetCalled },
		},
		{
			name: "DeleteNote",
			invoke: func(svc *app.NoteService, caller, setID uuid.UUID) error {
				return svc.DeleteNote(context.Background(), caller, setID, uuid.New())
			},
			ran: func(f *fakeNoteAgg) bool { return f.deleteCalled },
		},
	}
}

func TestNoteServiceAuthorization(t *testing.T) {
	for _, m := range noteMutations() {
		t.Run(m.name+"/author may mutate", func(t *testing.T) {
			caller := uuid.New()
			noteAgg := &fakeNoteAgg{author: caller}
			svc := app.NewNoteService(noteAgg)

			if err := m.invoke(svc, caller, uuid.New()); err != nil {
				t.Fatalf("%s() err = %v, want nil", m.name, err)
			}
			if !m.ran(noteAgg) {
				t.Errorf("%s did not reach the aggregate", m.name)
			}
		})

		t.Run(m.name+"/non-author is forbidden", func(t *testing.T) {
			noteAgg := &fakeNoteAgg{author: uuid.New()} // someone else
			svc := app.NewNoteService(noteAgg)

			err := m.invoke(svc, uuid.New(), uuid.New())
			if !errors.Is(err, domain.ErrForbidden) {
				t.Errorf("%s() err = %v, want ErrForbidden", m.name, err)
			}
			if m.ran(noteAgg) {
				t.Errorf("%s reached the aggregate despite being forbidden", m.name)
			}
		})

		t.Run(m.name+"/missing set is not found", func(t *testing.T) {
			noteAgg := &fakeNoteAgg{authorErr: domain.ErrNotFound}
			svc := app.NewNoteService(noteAgg)

			err := m.invoke(svc, uuid.New(), uuid.New())
			if !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("%s() err = %v, want ErrNotFound", m.name, err)
			}
			if m.ran(noteAgg) {
				t.Errorf("%s reached the aggregate despite a missing set", m.name)
			}
		})
	}
}
