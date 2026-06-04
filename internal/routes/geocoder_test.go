package routes_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/RecceLog/api/internal/domain"
	"github.com/RecceLog/api/internal/routes"
)

// fakeGeocoder records each call and returns programmed responses per point.
// EnrichCities reverse-geocodes the two endpoints concurrently, so calls is
// atomic to stay race-free under `go test -race`.
type fakeGeocoder struct {
	responses map[string]string // "lat,lng" → name
	calls     atomic.Int64
}

func (f *fakeGeocoder) ReverseGeocode(_ context.Context, lat, lng float64) (string, error) {
	f.calls.Add(1)
	key := fmt.Sprintf("%.2f,%.2f", lat, lng)
	return f.responses[key], nil
}

func TestEnrichCities(t *testing.T) {
	milanLat, milanLng := 45.46, 9.19
	romeLat, romeLng := 41.90, 12.49

	makePath := func() domain.LineString {
		return domain.LineString{
			{Lat: milanLat, Lng: milanLng},
			{Lat: romeLat, Lng: romeLng},
		}
	}

	t.Run("fills both cities when empty", func(t *testing.T) {
		geo := &fakeGeocoder{responses: map[string]string{
			fmt.Sprintf("%.2f,%.2f", milanLat, milanLng): "Milan",
			fmt.Sprintf("%.2f,%.2f", romeLat, romeLng):   "Rome",
		}}
		svc := routes.NewService(&fakeRepo{}, inlineTx{}, geo)

		r := domain.Route{
			Path:     makePath(),
			Vehicles: []domain.Vehicle{domain.VehicleCar},
		}
		svc.EnrichCities(context.Background(), &r)

		if r.StartCity != "Milan" {
			t.Errorf("StartCity = %q, want Milan", r.StartCity)
		}
		if r.FinishCity != "Rome" {
			t.Errorf("FinishCity = %q, want Rome", r.FinishCity)
		}
	})

	t.Run("does not overwrite cities the caller already supplied", func(t *testing.T) {
		geo := &fakeGeocoder{responses: map[string]string{}}
		svc := routes.NewService(&fakeRepo{}, inlineTx{}, geo)

		r := domain.Route{
			Path:       makePath(),
			Vehicles:   []domain.Vehicle{domain.VehicleCar},
			StartCity:  "Torino",
			FinishCity: "Napoli",
		}
		svc.EnrichCities(context.Background(), &r)

		if r.StartCity != "Torino" || r.FinishCity != "Napoli" {
			t.Errorf("cities overwritten: start=%q finish=%q", r.StartCity, r.FinishCity)
		}
		if n := geo.calls.Load(); n != 0 {
			t.Errorf("geocoder called %d times, want 0", n)
		}
	})

	t.Run("falls back to N/A when the geocoder returns nothing", func(t *testing.T) {
		geo := &fakeGeocoder{responses: map[string]string{}} // every lookup → ""
		svc := routes.NewService(&fakeRepo{}, inlineTx{}, geo)

		r := domain.Route{
			Path:     makePath(),
			Vehicles: []domain.Vehicle{domain.VehicleCar},
		}
		svc.EnrichCities(context.Background(), &r)

		if r.StartCity != "N/A" || r.FinishCity != "N/A" {
			t.Errorf("expected N/A placeholders, got start=%q finish=%q", r.StartCity, r.FinishCity)
		}
	})

	t.Run("nil geocoder fills the N/A placeholder", func(t *testing.T) {
		svc := routes.NewService(&fakeRepo{}, inlineTx{}, nil)

		r := domain.Route{
			Path:     makePath(),
			Vehicles: []domain.Vehicle{domain.VehicleCar},
		}
		svc.EnrichCities(context.Background(), &r)

		if r.StartCity != "N/A" || r.FinishCity != "N/A" {
			t.Errorf("expected N/A placeholders, got start=%q finish=%q", r.StartCity, r.FinishCity)
		}
	})
}
