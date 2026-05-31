package domain_test

import (
	"errors"
	"testing"

	"github.com/RecceLog/api/internal/domain"
)

func TestWaypointValidate(t *testing.T) {
	validPos := domain.Point{Lng: 9.19, Lat: 45.46}
	invalidPos := domain.Point{Lng: 200, Lat: 0}

	tests := []struct {
		name       string
		wp         domain.Waypoint
		wantErr    bool
		wantFields []string
	}{
		{
			name: "valid",
			wp:   domain.Waypoint{Position: validPos, Order: 0, Name: "start"},
		},
		{
			name:       "invalid position",
			wp:         domain.Waypoint{Position: invalidPos, Order: 1},
			wantErr:    true,
			wantFields: []string{"position"},
		},
		{
			name:       "negative order",
			wp:         domain.Waypoint{Position: validPos, Order: -1},
			wantErr:    true,
			wantFields: []string{"order"},
		},
		{
			name:       "both invalid",
			wp:         domain.Waypoint{Position: invalidPos, Order: -2},
			wantErr:    true,
			wantFields: []string{"position", "order"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.wp.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("Validate() = nil, want error")
				}
				if !errors.Is(err, domain.ErrValidation) {
					t.Errorf("errors.Is(err, ErrValidation) = false")
				}
				assertFields(t, err, tt.wantFields)
				return
			}
			if err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}
