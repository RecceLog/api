package users_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
	"github.com/RecceLog/api/internal/users"
)

// fakeRepo is a hand-rolled stand-in for users.Repository — each method
// delegates to a func field so tests program just the behavior they need.
type fakeRepo struct {
	getFn     func(ctx context.Context, sub string) (domain.User, error)
	getByIDFn func(ctx context.Context, id uuid.UUID) (domain.User, error)
	createFn  func(ctx context.Context, u domain.User) (domain.User, error)

	getCalls     int
	getByIDCalls int
	createCalls  int
}

func (f *fakeRepo) GetByKeycloakSub(ctx context.Context, sub string) (domain.User, error) {
	f.getCalls++
	return f.getFn(ctx, sub)
}

func (f *fakeRepo) GetByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	f.getByIDCalls++
	return f.getByIDFn(ctx, id)
}

func (f *fakeRepo) Create(ctx context.Context, u domain.User) (domain.User, error) {
	f.createCalls++
	return f.createFn(ctx, u)
}

func TestEnsureProvisioned(t *testing.T) {
	const sub = "kc-sub-123"

	t.Run("returns existing user without creating", func(t *testing.T) {
		want := domain.User{ID: uuid.New(), KeycloakSub: sub, DisplayName: "Ada"}
		repo := &fakeRepo{
			getFn: func(_ context.Context, gotSub string) (domain.User, error) {
				if gotSub != sub {
					t.Errorf("got sub %q, want %q", gotSub, sub)
				}
				return want, nil
			},
		}
		svc := users.NewService(repo)

		got, err := svc.EnsureProvisioned(context.Background(), sub, "Ada")
		if err != nil {
			t.Fatalf("EnsureProvisioned() err = %v, want nil", err)
		}
		if got.ID != want.ID {
			t.Errorf("got.ID = %v, want %v", got.ID, want.ID)
		}
		if repo.createCalls != 0 {
			t.Errorf("repo.Create calls = %d, want 0", repo.createCalls)
		}
	})

	t.Run("creates user on first sight", func(t *testing.T) {
		repo := &fakeRepo{
			getFn: func(_ context.Context, _ string) (domain.User, error) {
				return domain.User{}, domain.ErrNotFound
			},
			createFn: func(_ context.Context, u domain.User) (domain.User, error) {
				if u.KeycloakSub != sub || u.DisplayName != "Grace" {
					t.Errorf("Create got %+v, want sub=%q name=Grace", u, sub)
				}
				u.ID = uuid.New()
				return u, nil
			},
		}
		svc := users.NewService(repo)

		got, err := svc.EnsureProvisioned(context.Background(), sub, "Grace")
		if err != nil {
			t.Fatalf("EnsureProvisioned() err = %v, want nil", err)
		}
		if got.ID == uuid.Nil {
			t.Errorf("got.ID = nil, want assigned uuid")
		}
		if repo.createCalls != 1 {
			t.Errorf("repo.Create calls = %d, want 1", repo.createCalls)
		}
	})

	t.Run("falls back to subject when display name empty", func(t *testing.T) {
		repo := &fakeRepo{
			getFn: func(_ context.Context, _ string) (domain.User, error) {
				return domain.User{}, domain.ErrNotFound
			},
			createFn: func(_ context.Context, u domain.User) (domain.User, error) {
				if u.DisplayName != sub {
					t.Errorf("DisplayName = %q, want fallback %q", u.DisplayName, sub)
				}
				return u, nil
			},
		}
		svc := users.NewService(repo)

		if _, err := svc.EnsureProvisioned(context.Background(), sub, ""); err != nil {
			t.Fatalf("EnsureProvisioned() err = %v, want nil", err)
		}
	})

	t.Run("propagates non-NotFound lookup error without creating", func(t *testing.T) {
		boom := errors.New("db down")
		repo := &fakeRepo{
			getFn: func(_ context.Context, _ string) (domain.User, error) {
				return domain.User{}, boom
			},
		}
		svc := users.NewService(repo)

		_, err := svc.EnsureProvisioned(context.Background(), sub, "Ada")
		if !errors.Is(err, boom) {
			t.Errorf("err = %v, want %v", err, boom)
		}
		if repo.createCalls != 0 {
			t.Errorf("repo.Create calls = %d, want 0", repo.createCalls)
		}
	})
}
