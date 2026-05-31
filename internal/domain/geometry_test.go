package domain_test

import (
	"testing"

	"github.com/RecceLog/api/internal/domain"
)

func TestPointValid(t *testing.T) {
	tests := []struct {
		name  string
		point domain.Point
		want  bool
	}{
		{"origin", domain.Point{Lng: 0, Lat: 0}, true},
		{"max bounds", domain.Point{Lng: 180, Lat: 90}, true},
		{"min bounds", domain.Point{Lng: -180, Lat: -90}, true},
		{"lng too high", domain.Point{Lng: 180.1, Lat: 0}, false},
		{"lng too low", domain.Point{Lng: -180.1, Lat: 0}, false},
		{"lat too high", domain.Point{Lng: 0, Lat: 90.1}, false},
		{"lat too low", domain.Point{Lng: 0, Lat: -90.1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.point.Valid(); got != tt.want {
				t.Errorf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLineStringValid(t *testing.T) {
	validA := domain.Point{Lng: 9.19, Lat: 45.46}
	validB := domain.Point{Lng: 12.49, Lat: 41.90}
	invalid := domain.Point{Lng: 200, Lat: 0}

	tests := []struct {
		name string
		ls   domain.LineString
		want bool
	}{
		{"nil", nil, false},
		{"empty", domain.LineString{}, false},
		{"single point", domain.LineString{validA}, false},
		{"two valid points", domain.LineString{validA, validB}, true},
		{"contains invalid point", domain.LineString{validA, invalid, validB}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ls.Valid(); got != tt.want {
				t.Errorf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}
