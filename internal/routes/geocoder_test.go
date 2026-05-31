package routes_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
	"github.com/RecceLog/api/internal/routes"
)

// fakeGeocoder records each call and returns programmed responses per point.
type fakeGeocoder struct {
	responses map[string]string // "lat,lng" → name
	calls     int
}

func (f *fakeGeocoder) ReverseGeocode(_ context.Context, lat, lng float64) (string, error) {
	f.calls++
	key := fmt.Sprintf("%.2f,%.2f", lat, lng)
	return f.responses[key], nil
}

func TestEnrichCities(t *testing.T) {
	milanLat, milanLng := 45.46, 9.19
	romeLat, romeLng := 41.90, 12.49

	makeRepo := func() *fakeRepo {
		return &fakeRepo{
			createFn: func(_ context.Context, r domain.Route) (domain.Route, error) {
				r.ID = uuid.New()
				return r, nil
			},
		}
	}
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
		repo := makeRepo()
		svc := routes.NewService(repo, inlineTx{}, geo)

		got, err := svc.Create(context.Background(), domain.Route{
			Path:     makePath(),
			Vehicles: []domain.Vehicle{domain.VehicleCar},
		})
		if err != nil {
			t.Fatalf("Create() err = %v, want nil", err)
		}
		if got.StartCity != "Milan" {
			t.Errorf("StartCity = %q, want Milan", got.StartCity)
		}
		if got.FinishCity != "Rome" {
			t.Errorf("FinishCity = %q, want Rome", got.FinishCity)
		}
	})

	t.Run("does not overwrite cities the caller already supplied", func(t *testing.T) {
		geo := &fakeGeocoder{responses: map[string]string{}}
		repo := makeRepo()
		svc := routes.NewService(repo, inlineTx{}, geo)

		got, err := svc.Create(context.Background(), domain.Route{
			Path:       makePath(),
			Vehicles:   []domain.Vehicle{domain.VehicleCar},
			StartCity:  "Torino",
			FinishCity: "Napoli",
		})
		if err != nil {
			t.Fatalf("Create() err = %v, want nil", err)
		}
		if got.StartCity != "Torino" || got.FinishCity != "Napoli" {
			t.Errorf("cities overwritten: start=%q finish=%q", got.StartCity, got.FinishCity)
		}
		if geo.calls != 0 {
			t.Errorf("geocoder called %d times, want 0", geo.calls)
		}
	})

	t.Run("nil geocoder leaves cities empty without error", func(t *testing.T) {
		repo := makeRepo()
		svc := routes.NewService(repo, inlineTx{}, nil)

		got, err := svc.Create(context.Background(), domain.Route{
			Path:     makePath(),
			Vehicles: []domain.Vehicle{domain.VehicleCar},
		})
		if err != nil {
			t.Fatalf("Create() err = %v, want nil", err)
		}
		if got.StartCity != "" || got.FinishCity != "" {
			t.Errorf("expected empty cities, got start=%q finish=%q", got.StartCity, got.FinishCity)
		}
	})
}
