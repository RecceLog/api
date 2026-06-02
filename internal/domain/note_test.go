package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/RecceLog/api/internal/domain"
)

func TestNoteTypeValid(t *testing.T) {
	tests := []struct {
		t    domain.NoteType
		want bool
	}{
		{domain.NoteIndication, true},
		{domain.NoteWarning, true},
		{"", false},
		{"BOGUS", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.t), func(t *testing.T) {
			if got := tt.t.Valid(); got != tt.want {
				t.Errorf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDirectionTypeValid(t *testing.T) {
	tests := []struct {
		d    domain.DirectionType
		want bool
	}{
		{domain.DirectionLeft, true},
		{domain.DirectionRight, true},
		{domain.DirectionStraight, true},
		{domain.DirectionChicane, true},
		{"", false},
		{"BACK", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.d), func(t *testing.T) {
			if got := tt.d.Valid(); got != tt.want {
				t.Errorf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func intPtr(i int) *int { return &i }

func TestNoteValidate(t *testing.T) {
	validPos := domain.Point{Lng: 9.19, Lat: 45.46}
	invalidPos := domain.Point{Lng: 200, Lat: 0}

	tests := []struct {
		name       string
		note       domain.Note
		wantErr    bool
		wantFields []string
	}{
		{
			name: "valid indication",
			note: domain.Note{
				Position:  validPos,
				Order:     0,
				Type:      domain.NoteIndication,
				Direction: domain.DirectionLeft,
				Severity:  intPtr(3),
			},
		},
		{
			name: "valid warning without direction",
			note: domain.Note{
				Position: validPos,
				Order:    1,
				Type:     domain.NoteWarning,
			},
		},
		{
			name: "valid warning with direction",
			note: domain.Note{
				Position:  validPos,
				Order:     2,
				Type:      domain.NoteWarning,
				Direction: domain.DirectionStraight,
			},
		},
		{
			name:       "invalid position",
			note:       domain.Note{Position: invalidPos, Type: domain.NoteWarning},
			wantErr:    true,
			wantFields: []string{"position"},
		},
		{
			name:       "negative order",
			note:       domain.Note{Position: validPos, Order: -1, Type: domain.NoteWarning},
			wantErr:    true,
			wantFields: []string{"order"},
		},
		{
			name:       "unknown type",
			note:       domain.Note{Position: validPos, Type: "OTHER"},
			wantErr:    true,
			wantFields: []string{"type"},
		},
		{
			name: "severity below range",
			note: domain.Note{
				Position: validPos,
				Type:     domain.NoteWarning,
				Severity: intPtr(0),
			},
			wantErr:    true,
			wantFields: []string{"severity"},
		},
		{
			name: "severity above range",
			note: domain.Note{
				Position: validPos,
				Type:     domain.NoteWarning,
				Severity: intPtr(8),
			},
			wantErr:    true,
			wantFields: []string{"severity"},
		},
		{
			name:       "indication without direction",
			note:       domain.Note{Position: validPos, Type: domain.NoteIndication},
			wantErr:    true,
			wantFields: []string{"direction"},
		},
		{
			name: "indication with invalid direction",
			note: domain.Note{
				Position:  validPos,
				Type:      domain.NoteIndication,
				Direction: "DIAGONAL",
			},
			wantErr:    true,
			wantFields: []string{"direction"},
		},
		{
			name: "warning with invalid direction",
			note: domain.Note{
				Position:  validPos,
				Type:      domain.NoteWarning,
				Direction: "DIAGONAL",
			},
			wantErr:    true,
			wantFields: []string{"direction"},
		},
		{
			name: "description too long",
			note: domain.Note{
				Position:    validPos,
				Type:        domain.NoteWarning,
				Description: strings.Repeat("a", 256),
			},
			wantErr:    true,
			wantFields: []string{"description"},
		},
		{
			name: "description at limit",
			note: domain.Note{
				Position:    validPos,
				Type:        domain.NoteWarning,
				Description: strings.Repeat("a", 255),
			},
		},
		{
			name: "non-straight indication without severity",
			note: domain.Note{
				Position:  validPos,
				Type:      domain.NoteIndication,
				Direction: domain.DirectionLeft,
			},
			wantErr:    true,
			wantFields: []string{"severity"},
		},
		{
			name: "straight indication may omit severity",
			note: domain.Note{
				Position:  validPos,
				Type:      domain.NoteIndication,
				Direction: domain.DirectionStraight,
			},
		},
		{
			name: "non-straight warning may omit severity",
			note: domain.Note{
				Position:  validPos,
				Type:      domain.NoteWarning,
				Direction: domain.DirectionLeft,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.note.Validate()
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

func TestNoteSetValidate(t *testing.T) {
	validPos := domain.Point{Lng: 9.19, Lat: 45.46}
	invalidPos := domain.Point{Lng: 200, Lat: 0}

	validNote := domain.Note{Position: validPos, Type: domain.NoteWarning}
	brokenNote := domain.Note{Position: invalidPos, Type: domain.NoteWarning}

	t.Run("empty notes is invalid", func(t *testing.T) {
		ns := domain.NoteSet{}
		err := ns.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false")
		}
		assertFields(t, err, []string{"notes"})
	})

	t.Run("all valid", func(t *testing.T) {
		ns := domain.NoteSet{Notes: []domain.Note{validNote, validNote}}
		if err := ns.Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	})

	t.Run("wraps index of broken note", func(t *testing.T) {
		ns := domain.NoteSet{Notes: []domain.Note{validNote, brokenNote}}
		err := ns.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false")
		}
		if !strings.Contains(err.Error(), "notes[1]") {
			t.Errorf("expected error to reference notes[1], got: %v", err)
		}
	})

	t.Run("rejects an over-long name", func(t *testing.T) {
		ns := domain.NoteSet{Name: strings.Repeat("a", 121), Notes: []domain.Note{validNote}}
		err := ns.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		assertFields(t, err, []string{"name"})
	})

	t.Run("name at the limit is valid", func(t *testing.T) {
		ns := domain.NoteSet{Name: strings.Repeat("a", 120), Notes: []domain.Note{validNote}}
		if err := ns.Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	})
}
