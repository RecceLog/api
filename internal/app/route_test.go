package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/app"
	"github.com/RecceLog/api/internal/domain"
)

func validRoute() domain.Route {
	return domain.Route{
		Path: domain.LineString{
			{Lng: 9.19, Lat: 45.46},
			{Lng: 12.49, Lat: 41.90},
		},
		Vehicles: []domain.Vehicle{domain.VehicleCar},
	}
}

func validNoteSet() domain.NoteSet {
	return domain.NoteSet{
		Notes: []domain.Note{{Position: domain.Point{Lng: 9.19, Lat: 45.46}, Type: domain.NoteWarning}},
	}
}

func TestRouteServiceCreate(t *testing.T) {
	t.Run("rejects a nil note set without touching the aggregates", func(t *testing.T) {
		routeAgg := &fakeRouteAgg{}
		noteAgg := &fakeNoteAgg{}
		svc := app.NewRouteService(routeAgg, noteAgg, &inlineTx{})

		_, _, err := svc.Create(context.Background(), uuid.New(), validRoute(), nil)
		if err == nil {
			t.Fatal("Create() err = nil, want validation error")
		}
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false (err=%v)", err)
		}
		if routeAgg.createCalled || noteAgg.createNSCalled {
			t.Error("aggregates were touched despite a nil note set")
		}
	})

	t.Run("attributes route and note set to the caller and enriches before the tx", func(t *testing.T) {
		routeAgg := &fakeRouteAgg{}
		noteAgg := &fakeNoteAgg{}
		tx := &inlineTx{}
		svc := app.NewRouteService(routeAgg, noteAgg, tx)

		caller := uuid.New()
		ns := validNoteSet()

		route, noteSets, err := svc.Create(context.Background(), caller, validRoute(), &ns)
		if err != nil {
			t.Fatalf("Create() err = %v, want nil", err)
		}
		if !routeAgg.enrichCalled {
			t.Error("EnrichCities was not called")
		}
		if tx.calls != 1 {
			t.Errorf("InTx calls = %d, want 1", tx.calls)
		}
		if routeAgg.createdRoute.AuthorID != caller {
			t.Errorf("route AuthorID = %v, want caller %v", routeAgg.createdRoute.AuthorID, caller)
		}
		if noteAgg.createdNoteSet.AuthorID != caller {
			t.Errorf("note set AuthorID = %v, want caller %v", noteAgg.createdNoteSet.AuthorID, caller)
		}
		if noteAgg.createdNoteSet.RouteID != route.ID {
			t.Errorf("note set RouteID = %v, want route ID %v", noteAgg.createdNoteSet.RouteID, route.ID)
		}
		if len(noteSets) != 1 {
			t.Errorf("returned %d note sets, want 1", len(noteSets))
		}
	})
}

// routeMutation is one authorized route-mutating use case, plus a probe that
// reports whether the underlying aggregate method ran.
type routeMutation struct {
	name   string
	invoke func(svc *app.RouteService, caller, routeID uuid.UUID) error
	ran    func(f *fakeRouteAgg) bool
}

func routeMutations() []routeMutation {
	return []routeMutation{
		{
			name: "AddWaypoint",
			invoke: func(svc *app.RouteService, caller, routeID uuid.UUID) error {
				_, err := svc.AddWaypoint(context.Background(), caller, routeID, domain.Waypoint{Position: domain.Point{Lng: 1, Lat: 1}})
				return err
			},
			ran: func(f *fakeRouteAgg) bool { return f.addWaypointCalled },
		},
		{
			name: "UpdateWaypoint",
			invoke: func(svc *app.RouteService, caller, routeID uuid.UUID) error {
				_, err := svc.UpdateWaypoint(context.Background(), caller, routeID, domain.Waypoint{Position: domain.Point{Lng: 1, Lat: 1}})
				return err
			},
			ran: func(f *fakeRouteAgg) bool { return f.updateWaypointCalled },
		},
		{
			name: "DeleteWaypoint",
			invoke: func(svc *app.RouteService, caller, routeID uuid.UUID) error {
				return svc.DeleteWaypoint(context.Background(), caller, routeID, uuid.New())
			},
			ran: func(f *fakeRouteAgg) bool { return f.deleteWaypointCalled },
		},
		{
			name: "Delete",
			invoke: func(svc *app.RouteService, caller, routeID uuid.UUID) error {
				return svc.Delete(context.Background(), caller, routeID)
			},
			ran: func(f *fakeRouteAgg) bool { return f.deleteCalled },
		},
	}
}

func TestRouteServiceAuthorization(t *testing.T) {
	for _, m := range routeMutations() {
		t.Run(m.name+"/author may mutate", func(t *testing.T) {
			caller := uuid.New()
			routeAgg := &fakeRouteAgg{author: caller}
			svc := app.NewRouteService(routeAgg, &fakeNoteAgg{}, &inlineTx{})

			if err := m.invoke(svc, caller, uuid.New()); err != nil {
				t.Fatalf("%s() err = %v, want nil", m.name, err)
			}
			if !m.ran(routeAgg) {
				t.Errorf("%s did not reach the aggregate", m.name)
			}
		})

		t.Run(m.name+"/non-author is forbidden", func(t *testing.T) {
			routeAgg := &fakeRouteAgg{author: uuid.New()} // someone else
			svc := app.NewRouteService(routeAgg, &fakeNoteAgg{}, &inlineTx{})

			err := m.invoke(svc, uuid.New(), uuid.New())
			if !errors.Is(err, domain.ErrForbidden) {
				t.Errorf("%s() err = %v, want ErrForbidden", m.name, err)
			}
			if m.ran(routeAgg) {
				t.Errorf("%s reached the aggregate despite being forbidden", m.name)
			}
		})

		t.Run(m.name+"/missing route is not found", func(t *testing.T) {
			routeAgg := &fakeRouteAgg{authorErr: domain.ErrNotFound}
			svc := app.NewRouteService(routeAgg, &fakeNoteAgg{}, &inlineTx{})

			err := m.invoke(svc, uuid.New(), uuid.New())
			if !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("%s() err = %v, want ErrNotFound", m.name, err)
			}
			if m.ran(routeAgg) {
				t.Errorf("%s reached the aggregate despite a missing route", m.name)
			}
		})
	}
}

func TestRouteServiceAddNoteSetSkipsOwnership(t *testing.T) {
	// The deliberate exception: any authenticated user may add a note set to
	// any route. The route author must NOT be consulted, and the note set is
	// attributed to the caller.
	caller := uuid.New()
	routeAgg := &fakeRouteAgg{author: uuid.New()} // a different owner
	noteAgg := &fakeNoteAgg{}
	svc := app.NewRouteService(routeAgg, noteAgg, &inlineTx{})

	routeID := uuid.New()
	_, err := svc.AddNoteSet(context.Background(), caller, routeID, validNoteSet())
	if err != nil {
		t.Fatalf("AddNoteSet() err = %v, want nil", err)
	}
	if !noteAgg.createNSCalled {
		t.Fatal("AddNoteSet did not create the note set")
	}
	if noteAgg.createdNoteSet.AuthorID != caller {
		t.Errorf("note set AuthorID = %v, want caller %v", noteAgg.createdNoteSet.AuthorID, caller)
	}
	if noteAgg.createdNoteSet.RouteID != routeID {
		t.Errorf("note set RouteID = %v, want %v", noteAgg.createdNoteSet.RouteID, routeID)
	}
}
