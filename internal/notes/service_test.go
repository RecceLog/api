package notes_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
	"github.com/RecceLog/api/internal/notes"
)

// inlineTx is a hand-rolled stand-in for notes.Transactor. Runs fn inline.
// Atomicity gets verified at the integration-test layer.
type inlineTx struct{}

func (inlineTx) InTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type fakeRepo struct {
	createNoteSetFn func(ctx context.Context, ns domain.NoteSet) (domain.NoteSet, error)
	addNotesFn      func(ctx context.Context, setID uuid.UUID, ns []domain.Note) ([]domain.Note, error)
	listByRouteFn   func(ctx context.Context, routeID uuid.UUID) ([]domain.NoteSet, error)
	listBySetFn     func(ctx context.Context, setID uuid.UUID) ([]domain.Note, error)
	getSetAuthorFn  func(ctx context.Context, setID uuid.UUID) (uuid.UUID, error)
	updateNoteFn    func(ctx context.Context, setID uuid.UUID, n domain.Note) (domain.Note, error)
	deleteFn        func(ctx context.Context, setID uuid.UUID) error
	deleteNoteFn    func(ctx context.Context, setID, noteID uuid.UUID) error

	createNoteSetCalls int
	addNotesCalls      int
	listByRouteCalls   int
	listBySetCalls     int
	getSetAuthorCalls  int
	updateNoteCalls    int
	deleteCalls        int
	deleteNoteCalls    int
}

func (f *fakeRepo) CreateNoteSet(ctx context.Context, ns domain.NoteSet) (domain.NoteSet, error) {
	f.createNoteSetCalls++
	return f.createNoteSetFn(ctx, ns)
}

func (f *fakeRepo) AddNotes(ctx context.Context, setID uuid.UUID, ns []domain.Note) ([]domain.Note, error) {
	f.addNotesCalls++
	return f.addNotesFn(ctx, setID, ns)
}

func (f *fakeRepo) ListByRouteID(ctx context.Context, routeID uuid.UUID) ([]domain.NoteSet, error) {
	f.listByRouteCalls++
	return f.listByRouteFn(ctx, routeID)
}

func (f *fakeRepo) ListBySetID(ctx context.Context, setID uuid.UUID) ([]domain.Note, error) {
	f.listBySetCalls++
	return f.listBySetFn(ctx, setID)
}

func (f *fakeRepo) GetSetAuthor(ctx context.Context, setID uuid.UUID) (uuid.UUID, error) {
	f.getSetAuthorCalls++
	return f.getSetAuthorFn(ctx, setID)
}

func (f *fakeRepo) UpdateNote(ctx context.Context, setID uuid.UUID, n domain.Note) (domain.Note, error) {
	f.updateNoteCalls++
	return f.updateNoteFn(ctx, setID, n)
}

func (f *fakeRepo) DeleteNoteSet(ctx context.Context, setID uuid.UUID) error {
	f.deleteCalls++
	return f.deleteFn(ctx, setID)
}

func (f *fakeRepo) DeleteNote(ctx context.Context, setID, noteID uuid.UUID) error {
	f.deleteNoteCalls++
	return f.deleteNoteFn(ctx, setID, noteID)
}

func validNoteSet() domain.NoteSet {
	return domain.NoteSet{
		Notes: []domain.Note{{
			Position: domain.Point{Lng: 9.19, Lat: 45.46},
			Type:     domain.NoteWarning,
		}},
	}
}

func TestServiceCreateNoteSet(t *testing.T) {
	t.Run("persists note set and batches notes", func(t *testing.T) {
		setID := uuid.New()
		repo := &fakeRepo{
			createNoteSetFn: func(_ context.Context, ns domain.NoteSet) (domain.NoteSet, error) {
				ns.ID = setID
				return ns, nil
			},
			addNotesFn: func(_ context.Context, gotSetID uuid.UUID, n []domain.Note) ([]domain.Note, error) {
				if gotSetID != setID {
					t.Errorf("AddNotes got setID %v, want %v", gotSetID, setID)
				}
				return n, nil
			},
		}
		svc := notes.NewService(repo, inlineTx{})

		got, err := svc.CreateNoteSet(context.Background(), validNoteSet())
		if err != nil {
			t.Fatalf("CreateNoteSet() err = %v, want nil", err)
		}
		if got.ID != setID {
			t.Errorf("got.ID = %v, want %v", got.ID, setID)
		}
		if len(got.Notes) != 1 {
			t.Errorf("got %d notes, want 1", len(got.Notes))
		}
		if repo.createNoteSetCalls != 1 || repo.addNotesCalls != 1 {
			t.Errorf("calls = (%d, %d), want (1, 1)", repo.createNoteSetCalls, repo.addNotesCalls)
		}
	})

	t.Run("rejects invalid note set without touching repo", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := notes.NewService(repo, inlineTx{})

		_, err := svc.CreateNoteSet(context.Background(), domain.NoteSet{}) // empty notes
		if err == nil {
			t.Fatal("CreateNoteSet() err = nil, want validation error")
		}
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false")
		}
		if repo.createNoteSetCalls != 0 || repo.addNotesCalls != 0 {
			t.Errorf("calls = (%d, %d), want (0, 0)", repo.createNoteSetCalls, repo.addNotesCalls)
		}
	})

	t.Run("propagates CreateNoteSet error", func(t *testing.T) {
		boom := errors.New("db down")
		repo := &fakeRepo{
			createNoteSetFn: func(_ context.Context, _ domain.NoteSet) (domain.NoteSet, error) {
				return domain.NoteSet{}, boom
			},
		}
		svc := notes.NewService(repo, inlineTx{})

		_, err := svc.CreateNoteSet(context.Background(), validNoteSet())
		if !errors.Is(err, boom) {
			t.Errorf("err = %v, want %v", err, boom)
		}
	})

	t.Run("propagates AddNotes error", func(t *testing.T) {
		boom := errors.New("notes failed")
		repo := &fakeRepo{
			createNoteSetFn: func(_ context.Context, ns domain.NoteSet) (domain.NoteSet, error) {
				ns.ID = uuid.New()
				return ns, nil
			},
			addNotesFn: func(_ context.Context, _ uuid.UUID, _ []domain.Note) ([]domain.Note, error) {
				return nil, boom
			},
		}
		svc := notes.NewService(repo, inlineTx{})

		_, err := svc.CreateNoteSet(context.Background(), validNoteSet())
		if !errors.Is(err, boom) {
			t.Errorf("err = %v, want %v", err, boom)
		}
	})
}

func TestServiceListByRouteID(t *testing.T) {
	routeID := uuid.New()
	want := []domain.NoteSet{{ID: uuid.New()}, {ID: uuid.New()}}
	repo := &fakeRepo{
		listByRouteFn: func(_ context.Context, gotRouteID uuid.UUID) ([]domain.NoteSet, error) {
			if gotRouteID != routeID {
				t.Errorf("got routeID %v, want %v", gotRouteID, routeID)
			}
			return want, nil
		},
	}
	svc := notes.NewService(repo, inlineTx{})

	got, err := svc.ListByRouteID(context.Background(), routeID)
	if err != nil {
		t.Fatalf("ListByRouteID() err = %v, want nil", err)
	}
	if len(got) != len(want) {
		t.Errorf("len = %d, want %d", len(got), len(want))
	}
}

func TestServiceListBySetID(t *testing.T) {
	setID := uuid.New()
	want := []domain.Note{{ID: uuid.New(), Type: domain.NoteWarning}}
	repo := &fakeRepo{
		listBySetFn: func(_ context.Context, gotSetID uuid.UUID) ([]domain.Note, error) {
			if gotSetID != setID {
				t.Errorf("got setID %v, want %v", gotSetID, setID)
			}
			return want, nil
		},
	}
	svc := notes.NewService(repo, inlineTx{})

	got, err := svc.ListBySetID(context.Background(), setID)
	if err != nil {
		t.Fatalf("ListBySetID() err = %v, want nil", err)
	}
	if len(got) != len(want) {
		t.Errorf("len = %d, want %d", len(got), len(want))
	}
}

func TestServiceUpdateNote(t *testing.T) {
	setID := uuid.New()
	noteID := uuid.New()
	validNote := domain.Note{
		ID:       noteID,
		Position: domain.Point{Lng: 9.19, Lat: 45.46},
		Type:     domain.NoteWarning,
	}

	t.Run("delegates valid update to repo", func(t *testing.T) {
		repo := &fakeRepo{
			updateNoteFn: func(_ context.Context, gotSetID uuid.UUID, n domain.Note) (domain.Note, error) {
				if gotSetID != setID {
					t.Errorf("got setID %v, want %v", gotSetID, setID)
				}
				if n.ID != noteID {
					t.Errorf("got noteID %v, want %v", n.ID, noteID)
				}
				return n, nil
			},
		}
		svc := notes.NewService(repo, inlineTx{})

		got, err := svc.UpdateNote(context.Background(), setID, validNote)
		if err != nil {
			t.Fatalf("UpdateNote() err = %v, want nil", err)
		}
		if got.ID != noteID {
			t.Errorf("got.ID = %v, want %v", got.ID, noteID)
		}
		if repo.updateNoteCalls != 1 {
			t.Errorf("repo.UpdateNote calls = %d, want 1", repo.updateNoteCalls)
		}
	})

	t.Run("rejects invalid note without touching repo", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := notes.NewService(repo, inlineTx{})

		_, err := svc.UpdateNote(context.Background(), setID, domain.Note{ID: noteID}) // missing Type, invalid Position
		if err == nil {
			t.Fatal("UpdateNote() err = nil, want validation error")
		}
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false")
		}
		if repo.updateNoteCalls != 0 {
			t.Errorf("repo.UpdateNote calls = %d, want 0", repo.updateNoteCalls)
		}
	})

	t.Run("propagates ErrNotFound", func(t *testing.T) {
		repo := &fakeRepo{
			updateNoteFn: func(_ context.Context, _ uuid.UUID, _ domain.Note) (domain.Note, error) {
				return domain.Note{}, domain.ErrNotFound
			},
		}
		svc := notes.NewService(repo, inlineTx{})

		_, err := svc.UpdateNote(context.Background(), setID, validNote)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("errors.Is(err, ErrNotFound) = false, err = %v", err)
		}
	})
}

func TestServiceDeleteNoteSet(t *testing.T) {
	setID := uuid.New()
	repo := &fakeRepo{
		deleteFn: func(_ context.Context, gotSetID uuid.UUID) error {
			if gotSetID != setID {
				t.Errorf("got setID %v, want %v", gotSetID, setID)
			}
			return nil
		},
	}
	svc := notes.NewService(repo, inlineTx{})

	if err := svc.DeleteNoteSet(context.Background(), setID); err != nil {
		t.Fatalf("DeleteNoteSet() err = %v, want nil", err)
	}
}

func TestServiceDeleteNote(t *testing.T) {
	setID := uuid.New()
	noteID := uuid.New()

	t.Run("delegates to repo", func(t *testing.T) {
		repo := &fakeRepo{
			deleteNoteFn: func(_ context.Context, gotSet, gotNote uuid.UUID) error {
				if gotSet != setID || gotNote != noteID {
					t.Errorf("got (%v, %v), want (%v, %v)", gotSet, gotNote, setID, noteID)
				}
				return nil
			},
		}
		svc := notes.NewService(repo, inlineTx{})

		if err := svc.DeleteNote(context.Background(), setID, noteID); err != nil {
			t.Fatalf("DeleteNote() err = %v, want nil", err)
		}
		if repo.deleteNoteCalls != 1 {
			t.Errorf("repo.DeleteNote calls = %d, want 1", repo.deleteNoteCalls)
		}
	})

	t.Run("propagates ErrNotFound", func(t *testing.T) {
		repo := &fakeRepo{
			deleteNoteFn: func(_ context.Context, _, _ uuid.UUID) error {
				return domain.ErrNotFound
			},
		}
		svc := notes.NewService(repo, inlineTx{})

		err := svc.DeleteNote(context.Background(), setID, noteID)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("errors.Is(err, ErrNotFound) = false, err = %v", err)
		}
	})
}
