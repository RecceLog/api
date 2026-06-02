package routes_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
	"github.com/RecceLog/api/internal/routes"
	"github.com/RecceLog/api/internal/routes/dto"
)

// inlineTx is a hand-rolled stand-in for routes.Transactor.
// It runs fn directly without opening a real transaction — service tests only
// care that the orchestration shape is right; atomicity is verified at the
// integration-test layer (storage/postgres with a real DB).
type inlineTx struct{}

func (inlineTx) InTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// fakeRepo is a hand-rolled stand-in for routes.Repository.
// Each method delegates to a func field so individual tests program the
// behavior they need without dragging in a mocking library.
type fakeRepo struct {
	createFn          func(ctx context.Context, r domain.Route) (domain.Route, error)
	addWaypointsFn    func(ctx context.Context, routeID uuid.UUID, wps []domain.Waypoint) ([]domain.Waypoint, error)
	addWaypointFn     func(ctx context.Context, routeID uuid.UUID, w domain.Waypoint) (domain.Waypoint, error)
	updateWaypointFn  func(ctx context.Context, routeID uuid.UUID, w domain.Waypoint) (domain.Waypoint, error)
	deleteWaypointFn  func(ctx context.Context, routeID, waypointID uuid.UUID) error
	deleteFn          func(ctx context.Context, id uuid.UUID) error
	getDetailsByIDFn  func(ctx context.Context, id uuid.UUID) (domain.Route, error)
	getAuthorFn       func(ctx context.Context, id uuid.UUID) (uuid.UUID, error)
	listFn            func(ctx context.Context) ([]dto.RouteSummary, error)
	listNearbyFn      func(ctx context.Context, center domain.Point, radiusM float64) ([]dto.RouteSummary, error)
	listNearbyByVehFn func(ctx context.Context, center domain.Point, radiusM float64, vehicles []domain.Vehicle) ([]dto.RouteSummary, error)

	createCalls         int
	addWaypointsCalls   int
	addWaypointCalls    int
	updateWaypointCalls int
	deleteWaypointCalls int
	deleteCalls         int
	getDetailsByIDCalls int
	getAuthorCalls      int
	listCalls           int
	listNearbyCalls     int
	listNearbyByVehC    int
}

func (f *fakeRepo) Create(ctx context.Context, r domain.Route) (domain.Route, error) {
	f.createCalls++
	return f.createFn(ctx, r)
}

func (f *fakeRepo) AddWaypoints(ctx context.Context, routeID uuid.UUID, wps []domain.Waypoint) ([]domain.Waypoint, error) {
	f.addWaypointsCalls++
	return f.addWaypointsFn(ctx, routeID, wps)
}

func (f *fakeRepo) AddWaypoint(ctx context.Context, routeID uuid.UUID, w domain.Waypoint) (domain.Waypoint, error) {
	f.addWaypointCalls++
	return f.addWaypointFn(ctx, routeID, w)
}

func (f *fakeRepo) UpdateWaypoint(ctx context.Context, routeID uuid.UUID, w domain.Waypoint) (domain.Waypoint, error) {
	f.updateWaypointCalls++
	return f.updateWaypointFn(ctx, routeID, w)
}

func (f *fakeRepo) DeleteWaypoint(ctx context.Context, routeID, waypointID uuid.UUID) error {
	f.deleteWaypointCalls++
	return f.deleteWaypointFn(ctx, routeID, waypointID)
}

func (f *fakeRepo) Delete(ctx context.Context, id uuid.UUID) error {
	f.deleteCalls++
	return f.deleteFn(ctx, id)
}

func (f *fakeRepo) GetDetailsByID(ctx context.Context, id uuid.UUID) (domain.Route, error) {
	f.getDetailsByIDCalls++
	return f.getDetailsByIDFn(ctx, id)
}

func (f *fakeRepo) GetAuthor(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	f.getAuthorCalls++
	return f.getAuthorFn(ctx, id)
}

func (f *fakeRepo) List(ctx context.Context) ([]dto.RouteSummary, error) {
	f.listCalls++
	return f.listFn(ctx)
}

func (f *fakeRepo) ListNearby(ctx context.Context, center domain.Point, radiusM float64) ([]dto.RouteSummary, error) {
	f.listNearbyCalls++
	return f.listNearbyFn(ctx, center, radiusM)
}

func (f *fakeRepo) ListNearbyByVehicles(ctx context.Context, center domain.Point, radiusM float64, vehicles []domain.Vehicle) ([]dto.RouteSummary, error) {
	f.listNearbyByVehC++
	return f.listNearbyByVehFn(ctx, center, radiusM, vehicles)
}

func validRoute() domain.Route {
	return domain.Route{
		Path: domain.LineString{
			{Lng: 9.19, Lat: 45.46},
			{Lng: 12.49, Lat: 41.90},
		},
		Vehicles: []domain.Vehicle{domain.VehicleCar},
		NoteSets: []domain.NoteSet{
			{Notes: []domain.Note{{
				Position: domain.Point{Lng: 9.19, Lat: 45.46},
				Type:     domain.NoteWarning,
			}}},
		},
	}
}

func TestServiceCreate(t *testing.T) {
	t.Run("persists route and skips waypoints when none", func(t *testing.T) {
		want := validRoute()
		want.ID = uuid.New()

		repo := &fakeRepo{
			createFn: func(_ context.Context, _ domain.Route) (domain.Route, error) {
				return want, nil
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		got, err := svc.Create(context.Background(), validRoute())
		if err != nil {
			t.Fatalf("Create() err = %v, want nil", err)
		}
		if got.ID != want.ID {
			t.Errorf("got.ID = %v, want %v", got.ID, want.ID)
		}
		if repo.createCalls != 1 {
			t.Errorf("repo.Create calls = %d, want 1", repo.createCalls)
		}
		if repo.addWaypointsCalls != 0 {
			t.Errorf("repo.AddWaypoints calls = %d, want 0", repo.addWaypointsCalls)
		}
	})

	t.Run("batches waypoints when present", func(t *testing.T) {
		route := validRoute()
		route.Waypoints = []domain.Waypoint{
			{Position: domain.Point{Lng: 1, Lat: 1}, Order: 0},
			{Position: domain.Point{Lng: 2, Lat: 2}, Order: 1},
		}

		savedRoute := route
		savedRoute.ID = uuid.New()

		repo := &fakeRepo{
			createFn: func(_ context.Context, _ domain.Route) (domain.Route, error) {
				return savedRoute, nil
			},
			addWaypointsFn: func(_ context.Context, routeID uuid.UUID, wps []domain.Waypoint) ([]domain.Waypoint, error) {
				if routeID != savedRoute.ID {
					t.Errorf("AddWaypoints got routeID %v, want %v", routeID, savedRoute.ID)
				}
				if len(wps) != 2 {
					t.Errorf("AddWaypoints got %d wps, want 2", len(wps))
				}
				return wps, nil
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		got, err := svc.Create(context.Background(), route)
		if err != nil {
			t.Fatalf("Create() err = %v, want nil", err)
		}
		if len(got.Waypoints) != 2 {
			t.Errorf("got %d waypoints, want 2", len(got.Waypoints))
		}
		if repo.addWaypointsCalls != 1 {
			t.Errorf("repo.AddWaypoints calls = %d, want 1 (batched)", repo.addWaypointsCalls)
		}
	})

	t.Run("rejects invalid route without touching repo", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := routes.NewService(repo, inlineTx{}, nil)

		_, err := svc.Create(context.Background(), domain.Route{})
		if err == nil {
			t.Fatal("Create() err = nil, want validation error")
		}
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false")
		}
		if repo.createCalls != 0 {
			t.Errorf("repo.Create calls = %d, want 0", repo.createCalls)
		}
	})

	t.Run("propagates repo.Create error", func(t *testing.T) {
		boom := errors.New("db down")
		repo := &fakeRepo{
			createFn: func(_ context.Context, _ domain.Route) (domain.Route, error) {
				return domain.Route{}, boom
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		_, err := svc.Create(context.Background(), validRoute())
		if !errors.Is(err, boom) {
			t.Errorf("err = %v, want %v", err, boom)
		}
	})

	t.Run("propagates repo.AddWaypoints error", func(t *testing.T) {
		boom := errors.New("waypoints failed")
		route := validRoute()
		route.Waypoints = []domain.Waypoint{{Position: domain.Point{Lng: 1, Lat: 1}}}

		repo := &fakeRepo{
			createFn: func(_ context.Context, r domain.Route) (domain.Route, error) {
				r.ID = uuid.New()
				return r, nil
			},
			addWaypointsFn: func(_ context.Context, _ uuid.UUID, _ []domain.Waypoint) ([]domain.Waypoint, error) {
				return nil, boom
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		_, err := svc.Create(context.Background(), route)
		if !errors.Is(err, boom) {
			t.Errorf("err = %v, want %v", err, boom)
		}
	})
}

func TestServiceGetDetailsByID(t *testing.T) {
	t.Run("returns route from repo", func(t *testing.T) {
		id := uuid.New()
		want := validRoute()
		want.ID = id

		repo := &fakeRepo{
			getDetailsByIDFn: func(_ context.Context, gotID uuid.UUID) (domain.Route, error) {
				if gotID != id {
					t.Errorf("repo got id %v, want %v", gotID, id)
				}
				return want, nil
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		got, err := svc.GetDetailsByID(context.Background(), id)
		if err != nil {
			t.Fatalf("GetDetailsByID() err = %v, want nil", err)
		}
		if got.ID != id {
			t.Errorf("got.ID = %v, want %v", got.ID, id)
		}
	})

	t.Run("propagates ErrNotFound", func(t *testing.T) {
		repo := &fakeRepo{
			getDetailsByIDFn: func(_ context.Context, _ uuid.UUID) (domain.Route, error) {
				return domain.Route{}, domain.ErrNotFound
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		_, err := svc.GetDetailsByID(context.Background(), uuid.New())
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("errors.Is(err, ErrNotFound) = false, err = %v", err)
		}
	})
}

func TestServiceList(t *testing.T) {
	want := []dto.RouteSummary{{ID: uuid.New()}, {ID: uuid.New()}}
	repo := &fakeRepo{
		listFn: func(_ context.Context) ([]dto.RouteSummary, error) { return want, nil },
	}
	svc := routes.NewService(repo, inlineTx{}, nil)

	got, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List() err = %v, want nil", err)
	}
	if len(got) != len(want) {
		t.Errorf("len = %d, want %d", len(got), len(want))
	}
}

func TestServiceListNearby(t *testing.T) {
	validCenter := domain.Point{Lng: 9.19, Lat: 45.46}

	t.Run("delegates to repo when input is valid", func(t *testing.T) {
		want := []dto.RouteSummary{{ID: uuid.New()}}
		repo := &fakeRepo{
			listNearbyFn: func(_ context.Context, c domain.Point, r float64) ([]dto.RouteSummary, error) {
				if c != validCenter || r != 1000 {
					t.Errorf("repo got (%v, %v), want (%v, 1000)", c, r, validCenter)
				}
				return want, nil
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		got, err := svc.ListNearby(context.Background(), validCenter, 1000)
		if err != nil {
			t.Fatalf("ListNearby() err = %v, want nil", err)
		}
		if len(got) != len(want) {
			t.Errorf("len(got) = %d, want %d", len(got), len(want))
		}
	})

	t.Run("rejects invalid center", func(t *testing.T) {
		svc := routes.NewService(&fakeRepo{}, inlineTx{}, nil)
		_, err := svc.ListNearby(context.Background(), domain.Point{Lng: 200, Lat: 0}, 1000)
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false, err = %v", err)
		}
	})

	t.Run("rejects non-positive radius", func(t *testing.T) {
		svc := routes.NewService(&fakeRepo{}, inlineTx{}, nil)
		_, err := svc.ListNearby(context.Background(), validCenter, 0)
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false, err = %v", err)
		}
	})

	t.Run("rejects oversized radius", func(t *testing.T) {
		svc := routes.NewService(&fakeRepo{}, inlineTx{}, nil)
		_, err := svc.ListNearby(context.Background(), validCenter, 1_000_000)
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false, err = %v", err)
		}
	})

	t.Run("does not call repo on invalid input", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := routes.NewService(repo, inlineTx{}, nil)
		_, _ = svc.ListNearby(context.Background(), validCenter, -1)
		if repo.listNearbyCalls != 0 {
			t.Errorf("repo.ListNearby calls = %d, want 0", repo.listNearbyCalls)
		}
	})
}

func TestServiceListNearbyByVehicles(t *testing.T) {
	validCenter := domain.Point{Lng: 9.19, Lat: 45.46}

	t.Run("filters by vehicles when provided", func(t *testing.T) {
		want := []dto.RouteSummary{{ID: uuid.New()}}
		repo := &fakeRepo{
			listNearbyByVehFn: func(_ context.Context, _ domain.Point, _ float64, vs []domain.Vehicle) ([]dto.RouteSummary, error) {
				if len(vs) != 2 {
					t.Errorf("got %d vehicles, want 2", len(vs))
				}
				return want, nil
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		got, err := svc.ListNearbyByVehicles(context.Background(), validCenter, 1000,
			[]domain.Vehicle{domain.VehicleCar, domain.VehicleBike})
		if err != nil {
			t.Fatalf("ListNearbyByVehicles() err = %v, want nil", err)
		}
		if len(got) != len(want) {
			t.Errorf("len(got) = %d, want %d", len(got), len(want))
		}
		if repo.listNearbyByVehC != 1 {
			t.Errorf("listNearbyByVehicles calls = %d, want 1", repo.listNearbyByVehC)
		}
	})

	t.Run("falls back to unfiltered ListNearby when no vehicles", func(t *testing.T) {
		repo := &fakeRepo{
			listNearbyFn: func(_ context.Context, _ domain.Point, _ float64) ([]dto.RouteSummary, error) {
				return nil, nil
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		if _, err := svc.ListNearbyByVehicles(context.Background(), validCenter, 1000, nil); err != nil {
			t.Fatalf("ListNearbyByVehicles() err = %v, want nil", err)
		}
		if repo.listNearbyCalls != 1 || repo.listNearbyByVehC != 0 {
			t.Errorf("calls = (nearby %d, byVeh %d), want (1, 0)", repo.listNearbyCalls, repo.listNearbyByVehC)
		}
	})

	t.Run("rejects invalid vehicle without touching repo", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := routes.NewService(repo, inlineTx{}, nil)

		_, err := svc.ListNearbyByVehicles(context.Background(), validCenter, 1000,
			[]domain.Vehicle{"PLANE"})
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false, err = %v", err)
		}
		if repo.listNearbyByVehC != 0 {
			t.Errorf("listNearbyByVehicles calls = %d, want 0", repo.listNearbyByVehC)
		}
	})

	t.Run("rejects invalid spatial input before vehicles", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := routes.NewService(repo, inlineTx{}, nil)
		_, err := svc.ListNearbyByVehicles(context.Background(), validCenter, -1,
			[]domain.Vehicle{domain.VehicleCar})
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false, err = %v", err)
		}
	})
}

func TestServiceDeleteWaypoint(t *testing.T) {
	routeID := uuid.New()
	waypointID := uuid.New()

	t.Run("delegates to repo", func(t *testing.T) {
		repo := &fakeRepo{
			deleteWaypointFn: func(_ context.Context, gotRoute, gotWp uuid.UUID) error {
				if gotRoute != routeID || gotWp != waypointID {
					t.Errorf("got (%v, %v), want (%v, %v)", gotRoute, gotWp, routeID, waypointID)
				}
				return nil
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		if err := svc.DeleteWaypoint(context.Background(), routeID, waypointID); err != nil {
			t.Fatalf("DeleteWaypoint() err = %v, want nil", err)
		}
		if repo.deleteWaypointCalls != 1 {
			t.Errorf("repo.DeleteWaypoint calls = %d, want 1", repo.deleteWaypointCalls)
		}
	})

	t.Run("propagates ErrNotFound", func(t *testing.T) {
		repo := &fakeRepo{
			deleteWaypointFn: func(_ context.Context, _, _ uuid.UUID) error {
				return domain.ErrNotFound
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		err := svc.DeleteWaypoint(context.Background(), routeID, waypointID)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("errors.Is(err, ErrNotFound) = false, err = %v", err)
		}
	})
}

func TestServiceAddWaypoint(t *testing.T) {
	routeID := uuid.New()
	validWp := domain.Waypoint{Position: domain.Point{Lng: 9.19, Lat: 45.46}, Order: 0, Name: "start"}

	t.Run("delegates valid waypoint to repo", func(t *testing.T) {
		repo := &fakeRepo{
			addWaypointFn: func(_ context.Context, gotRouteID uuid.UUID, w domain.Waypoint) (domain.Waypoint, error) {
				if gotRouteID != routeID {
					t.Errorf("got routeID %v, want %v", gotRouteID, routeID)
				}
				w.ID = uuid.New()
				return w, nil
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		got, err := svc.AddWaypoint(context.Background(), routeID, validWp)
		if err != nil {
			t.Fatalf("AddWaypoint() err = %v, want nil", err)
		}
		if got.ID == uuid.Nil {
			t.Errorf("got.ID = nil, want assigned uuid")
		}
		if repo.addWaypointCalls != 1 {
			t.Errorf("repo.AddWaypoint calls = %d, want 1", repo.addWaypointCalls)
		}
	})

	t.Run("rejects invalid waypoint without touching repo", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := routes.NewService(repo, inlineTx{}, nil)

		_, err := svc.AddWaypoint(context.Background(), routeID, domain.Waypoint{Order: -1})
		if err == nil {
			t.Fatal("AddWaypoint() err = nil, want validation error")
		}
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false")
		}
		if repo.addWaypointCalls != 0 {
			t.Errorf("repo.AddWaypoint calls = %d, want 0", repo.addWaypointCalls)
		}
	})

	t.Run("propagates ErrNotFound when route is missing", func(t *testing.T) {
		repo := &fakeRepo{
			addWaypointFn: func(_ context.Context, _ uuid.UUID, _ domain.Waypoint) (domain.Waypoint, error) {
				return domain.Waypoint{}, domain.ErrNotFound
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		_, err := svc.AddWaypoint(context.Background(), routeID, validWp)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("errors.Is(err, ErrNotFound) = false, err = %v", err)
		}
	})
}

func TestServiceUpdateWaypoint(t *testing.T) {
	routeID := uuid.New()
	waypointID := uuid.New()
	validWp := domain.Waypoint{ID: waypointID, Position: domain.Point{Lng: 9.19, Lat: 45.46}, Order: 1, Name: "edited"}

	t.Run("delegates valid update to repo", func(t *testing.T) {
		repo := &fakeRepo{
			updateWaypointFn: func(_ context.Context, gotRouteID uuid.UUID, w domain.Waypoint) (domain.Waypoint, error) {
				if gotRouteID != routeID {
					t.Errorf("got routeID %v, want %v", gotRouteID, routeID)
				}
				if w.ID != waypointID {
					t.Errorf("got waypointID %v, want %v", w.ID, waypointID)
				}
				return w, nil
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		got, err := svc.UpdateWaypoint(context.Background(), routeID, validWp)
		if err != nil {
			t.Fatalf("UpdateWaypoint() err = %v, want nil", err)
		}
		if got.Name != "edited" {
			t.Errorf("got.Name = %q, want %q", got.Name, "edited")
		}
	})

	t.Run("rejects invalid waypoint without touching repo", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := routes.NewService(repo, inlineTx{}, nil)

		_, err := svc.UpdateWaypoint(context.Background(), routeID, domain.Waypoint{ID: waypointID, Position: domain.Point{Lng: 200}})
		if err == nil {
			t.Fatal("UpdateWaypoint() err = nil, want validation error")
		}
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false")
		}
		if repo.updateWaypointCalls != 0 {
			t.Errorf("repo.UpdateWaypoint calls = %d, want 0", repo.updateWaypointCalls)
		}
	})

	t.Run("propagates ErrNotFound on wrong pair", func(t *testing.T) {
		repo := &fakeRepo{
			updateWaypointFn: func(_ context.Context, _ uuid.UUID, _ domain.Waypoint) (domain.Waypoint, error) {
				return domain.Waypoint{}, domain.ErrNotFound
			},
		}
		svc := routes.NewService(repo, inlineTx{}, nil)

		_, err := svc.UpdateWaypoint(context.Background(), routeID, validWp)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("errors.Is(err, ErrNotFound) = false, err = %v", err)
		}
	})
}

func TestServiceDelete(t *testing.T) {
	id := uuid.New()
	repo := &fakeRepo{
		deleteFn: func(_ context.Context, gotID uuid.UUID) error {
			if gotID != id {
				t.Errorf("repo got id %v, want %v", gotID, id)
			}
			return nil
		},
	}
	svc := routes.NewService(repo, inlineTx{}, nil)

	if err := svc.Delete(context.Background(), id); err != nil {
		t.Fatalf("Delete() err = %v, want nil", err)
	}
}
