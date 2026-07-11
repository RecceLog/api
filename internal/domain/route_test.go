package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/RecceLog/api/internal/domain"
)

func TestVehicleValid(t *testing.T) {
	tests := []struct {
		v    domain.Vehicle
		want bool
	}{
		{domain.VehicleFoot, true},
		{domain.VehicleBike, true},
		{domain.VehicleMotorcycle, true},
		{domain.VehicleCar, true},
		{"", false},
		{"PLANE", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.v), func(t *testing.T) {
			if got := tt.v.Valid(); got != tt.want {
				t.Errorf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRouteValidate(t *testing.T) {
	validPath := domain.LineString{
		{Lng: 9.19, Lat: 45.46},
		{Lng: 12.49, Lat: 41.90},
	}
	invalidPath := domain.LineString{{Lng: 9.19, Lat: 45.46}} // only one point
	validPos := domain.Point{Lng: 9.19, Lat: 45.46}
	invalidPos := domain.Point{Lng: 200, Lat: 0}

	validNote := domain.Note{Position: validPos, Type: domain.NoteWarning}
	validNoteSet := domain.NoteSet{Notes: []domain.Note{validNote}}

	t.Run("valid minimal route", func(t *testing.T) {
		r := domain.Route{
			Path:     validPath,
			Vehicles: []domain.Vehicle{domain.VehicleCar},
			NoteSets: []domain.NoteSet{validNoteSet},
		}
		if err := r.Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	})

	t.Run("invalid path", func(t *testing.T) {
		r := domain.Route{
			Path:     invalidPath,
			Vehicles: []domain.Vehicle{domain.VehicleCar},
			NoteSets: []domain.NoteSet{validNoteSet},
		}
		err := r.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		assertFields(t, err, []string{"path"})
	})

	t.Run("no vehicles", func(t *testing.T) {
		r := domain.Route{
			Path:     validPath,
			NoteSets: []domain.NoteSet{validNoteSet},
		}
		err := r.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		assertFields(t, err, []string{"vehicles"})
	})

	t.Run("invalid vehicle in list", func(t *testing.T) {
		r := domain.Route{
			Path:     validPath,
			Vehicles: []domain.Vehicle{domain.VehicleCar, "BOAT"},
			NoteSets: []domain.NoteSet{validNoteSet},
		}
		err := r.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		if !strings.Contains(err.Error(), "vehicles[1]") {
			t.Errorf("expected error to reference vehicles[1], got: %v", err)
		}
	})

	t.Run("rejects duplicate vehicles", func(t *testing.T) {
		r := domain.Route{
			Path:     validPath,
			Vehicles: []domain.Vehicle{domain.VehicleCar, domain.VehicleCar},
			NoteSets: []domain.NoteSet{validNoteSet},
		}
		err := r.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		if !strings.Contains(err.Error(), "duplicate") {
			t.Errorf("expected a duplicate-vehicle error, got: %v", err)
		}
	})

	t.Run("rejects more than four vehicles", func(t *testing.T) {
		r := domain.Route{
			Path: validPath,
			Vehicles: []domain.Vehicle{
				domain.VehicleFoot, domain.VehicleBike,
				domain.VehicleMotorcycle, domain.VehicleCar, domain.VehicleFoot,
			},
			NoteSets: []domain.NoteSet{validNoteSet},
		}
		err := r.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		assertFields(t, err, []string{"vehicles", "vehicles[4]"})
	})

	t.Run("accepts the four distinct vehicles", func(t *testing.T) {
		r := domain.Route{
			Path: validPath,
			Vehicles: []domain.Vehicle{
				domain.VehicleFoot, domain.VehicleBike,
				domain.VehicleMotorcycle, domain.VehicleCar,
			},
			NoteSets: []domain.NoteSet{validNoteSet},
		}
		if err := r.Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	})

	t.Run("no note sets is valid (creation rule enforced at HTTP)", func(t *testing.T) {
		r := domain.Route{
			Path:     validPath,
			Vehicles: []domain.Vehicle{domain.VehicleCar},
		}
		if err := r.Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	})

	t.Run("wraps invalid waypoint index", func(t *testing.T) {
		r := domain.Route{
			Path:     validPath,
			Vehicles: []domain.Vehicle{domain.VehicleCar},
			NoteSets: []domain.NoteSet{validNoteSet},
			Waypoints: []domain.Waypoint{
				{Position: validPos},
				{Position: invalidPos},
			},
		}
		err := r.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false")
		}
		if !strings.Contains(err.Error(), "waypoints[1]") {
			t.Errorf("expected error to reference waypoints[1], got: %v", err)
		}
	})

	t.Run("rejects over-long text fields", func(t *testing.T) {
		r := domain.Route{
			Path:          validPath,
			Vehicles:      []domain.Vehicle{domain.VehicleCar},
			NoteSets:      []domain.NoteSet{validNoteSet},
			Name:        strings.Repeat("a", 121),
			Description: strings.Repeat("b", 1001),
			StartCity:   strings.Repeat("d", 121),
			FinishCity:  strings.Repeat("e", 121),
		}
		err := r.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		assertFields(t, err, []string{"name", "description", "startCity", "finishCity"})
	})

	t.Run("text fields at the limit are valid", func(t *testing.T) {
		r := domain.Route{
			Path:          validPath,
			Vehicles:      []domain.Vehicle{domain.VehicleCar},
			NoteSets:      []domain.NoteSet{validNoteSet},
			Name:        strings.Repeat("a", 120),
			Description: strings.Repeat("b", 1000),
			StartCity:   strings.Repeat("d", 120),
			FinishCity:  strings.Repeat("e", 120),
		}
		if err := r.Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	})

	t.Run("rejects a path with too many points", func(t *testing.T) {
		path := make(domain.LineString, 5001)
		for i := range path {
			path[i] = domain.Point{Lng: 9.0, Lat: 45.0}
		}
		r := domain.Route{
			Path:     path,
			Vehicles: []domain.Vehicle{domain.VehicleCar},
			NoteSets: []domain.NoteSet{validNoteSet},
		}
		err := r.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		assertFields(t, err, []string{"path"})
	})

	t.Run("wraps invalid note set index", func(t *testing.T) {
		brokenNote := domain.Note{Position: invalidPos, Type: domain.NoteWarning}
		r := domain.Route{
			Path:     validPath,
			Vehicles: []domain.Vehicle{domain.VehicleCar},
			NoteSets: []domain.NoteSet{
				validNoteSet,
				{Notes: []domain.Note{brokenNote}},
			},
		}
		err := r.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		if !strings.Contains(err.Error(), "noteSets[1]") {
			t.Errorf("expected error to reference noteSets[1], got: %v", err)
		}
	})
}
