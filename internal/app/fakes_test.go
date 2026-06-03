package app_test

import (
	"context"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
	routesdto "github.com/RecceLog/api/internal/routes/dto"
)

// inlineTx is a Transactor that runs fn directly — no real transaction needed
// for unit tests of the orchestration/authorization logic.
type inlineTx struct{ calls int }

func (t *inlineTx) InTx(ctx context.Context, fn func(context.Context) error) error {
	t.calls++
	return fn(ctx)
}

// fakeRouteAgg is a programmable RouteAggregate. author/authorErr drive the
// authorization lookups; the *Called flags record whether the mutating method
// ran, so a test can prove an unauthorized call never reaches the aggregate.
type fakeRouteAgg struct {
	author    uuid.UUID
	authorErr error

	enrichCalled bool

	createdRoute domain.Route
	createCalled bool

	addWaypointCalled    bool
	updateWaypointCalled bool
	deleteWaypointCalled bool
	deleteCalled         bool
}

func (f *fakeRouteAgg) Create(_ context.Context, r domain.Route) (domain.Route, error) {
	f.createCalled = true
	f.createdRoute = r
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return r, nil
}

func (f *fakeRouteAgg) GetDetailsByID(_ context.Context, _ uuid.UUID) (domain.Route, error) {
	return domain.Route{}, nil
}

func (f *fakeRouteAgg) List(_ context.Context) ([]routesdto.RouteSummary, error) {
	return nil, nil
}

func (f *fakeRouteAgg) GetAuthor(_ context.Context, _ uuid.UUID) (uuid.UUID, error) {
	return f.author, f.authorErr
}

func (f *fakeRouteAgg) ListNearbyByVehicles(_ context.Context, _ domain.Point, _ float64, _ []domain.Vehicle) ([]routesdto.RouteSummary, error) {
	return nil, nil
}

func (f *fakeRouteAgg) AddWaypoint(_ context.Context, _ uuid.UUID, w domain.Waypoint) (domain.Waypoint, error) {
	f.addWaypointCalled = true
	return w, nil
}

func (f *fakeRouteAgg) UpdateWaypoint(_ context.Context, _ uuid.UUID, w domain.Waypoint) (domain.Waypoint, error) {
	f.updateWaypointCalled = true
	return w, nil
}

func (f *fakeRouteAgg) DeleteWaypoint(_ context.Context, _, _ uuid.UUID) error {
	f.deleteWaypointCalled = true
	return nil
}

func (f *fakeRouteAgg) Delete(_ context.Context, _ uuid.UUID) error {
	f.deleteCalled = true
	return nil
}

func (f *fakeRouteAgg) EnrichCities(_ context.Context, _ *domain.Route) {
	f.enrichCalled = true
}

// fakeNoteAgg is a programmable NoteAggregate, mirroring fakeRouteAgg.
type fakeNoteAgg struct {
	author    uuid.UUID
	authorErr error

	createdNoteSet  domain.NoteSet
	createNSCalled  bool
	addNotesCalled  bool
	updateCalled    bool
	deleteSetCalled bool
	deleteCalled    bool
}

func (f *fakeNoteAgg) CreateNoteSet(_ context.Context, ns domain.NoteSet) (domain.NoteSet, error) {
	f.createNSCalled = true
	f.createdNoteSet = ns
	if ns.ID == uuid.Nil {
		ns.ID = uuid.New()
	}
	return ns, nil
}

func (f *fakeNoteAgg) ListByRouteID(_ context.Context, _ uuid.UUID) ([]domain.NoteSet, error) {
	return nil, nil
}

func (f *fakeNoteAgg) ListBySetID(_ context.Context, _ uuid.UUID) ([]domain.Note, error) {
	return nil, nil
}

func (f *fakeNoteAgg) GetSetAuthor(_ context.Context, _ uuid.UUID) (uuid.UUID, error) {
	return f.author, f.authorErr
}

func (f *fakeNoteAgg) AddNotes(_ context.Context, _ uuid.UUID, ns []domain.Note) ([]domain.Note, error) {
	f.addNotesCalled = true
	return ns, nil
}

func (f *fakeNoteAgg) UpdateNote(_ context.Context, _ uuid.UUID, n domain.Note) (domain.Note, error) {
	f.updateCalled = true
	return n, nil
}

func (f *fakeNoteAgg) DeleteNoteSet(_ context.Context, _ uuid.UUID) error {
	f.deleteSetCalled = true
	return nil
}

func (f *fakeNoteAgg) DeleteNote(_ context.Context, _, _ uuid.UUID) error {
	f.deleteCalled = true
	return nil
}
