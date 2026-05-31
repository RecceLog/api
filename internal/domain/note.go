package domain

import (
	"errors"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// NoteType distinguishes between a route instruction and a warning.
type NoteType string

const (
	NoteIndication NoteType = "INDICATION"
	NoteWarning    NoteType = "WARNING"
)

// Valid verifies that the note type is known.
func (t NoteType) Valid() bool {
	switch t {
	case NoteIndication, NoteWarning:
		return true
	default:
		return false
	}
}

// DirectionType is the direction associated with the note indication.
// It is mandatory if the note type is "INDICATION".
type DirectionType string

const (
	DirectionLeft     DirectionType = "LEFT"
	DirectionRight    DirectionType = "RIGHT"
	DirectionStraight DirectionType = "STRAIGHT"
	DirectionChicane  DirectionType = "CHICANE"
)

// Valid verifies that the direction is known.
func (d DirectionType) Valid() bool {
	switch d {
	case DirectionLeft, DirectionRight, DirectionStraight, DirectionChicane:
		return true
	default:
		return false
	}
}

const (
	noteDescriptionMaxLen = 255
	minIndicationSeverity = 1
	maxIndicationSeverity = 7
)

// NoteSet gathers notes of a route.
// NoteSets must reference a valid route, a valid author
// and have at least one valid note inside.
type NoteSet struct {
	ID        uuid.UUID
	Name      string
	Notes     []Note
	RouteID   uuid.UUID
	AuthorID  uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Validate verifies that every data inside a note set, including each note, is valid
func (s NoteSet) Validate() error {
	var errs []error
	if len(s.Notes) == 0 {
		errs = append(errs, invalid("notes", "specify at least one note"))
	}
	for i, n := range s.Notes {
		if err := n.Validate(); err != nil {
			errs = append(errs, wrapIndex("notes", i, err))
		}
	}
	return errors.Join(errs...)
}

// Note is a georeferenced note on the route.
// A note must have:
//   - a position to show it on the map,
//   - an order to show an ordered list of notes (author can eventually change the order),
//   - a type describing if the note is an indication or a warning,
//   - an optional severity included between 1 and 7,
//   - an optional direction
//   - an optional description
type Note struct {
	ID          uuid.UUID
	Position    Point
	Order       int
	Type        NoteType
	Severity    *int
	Direction   DirectionType
	Description string
}

// Validate verifies that all note values are consistent
func (n Note) Validate() error {
	var errs []error

	if !n.Position.Valid() {
		errs = append(errs, invalid("position", "invalid coordinates"))
	}
	if n.Order < 0 {
		errs = append(errs, invalid("order", "can't be negative"))
	}
	if !n.Type.Valid() {
		errs = append(errs, invalid("type", "invalid type: %q", n.Type))
	}
	if n.Severity != nil && (*n.Severity < minIndicationSeverity || *n.Severity > maxIndicationSeverity) {
		errs = append(errs, invalid("severity", "must be between %d and %d", minIndicationSeverity, maxIndicationSeverity))
	}

	switch n.Type {
	case NoteIndication:
		switch {
		case n.Direction == "":
			errs = append(errs, invalid("direction", "mandatory for indication"))
		case !n.Direction.Valid():
			errs = append(errs, invalid("direction", "not valid: %q", n.Direction))
		}
	case NoteWarning:
		if n.Direction != "" && !n.Direction.Valid() {
			errs = append(errs, invalid("direction", "not valid: %q", n.Direction))
		}
	}

	if utf8.RuneCountInString(n.Description) > noteDescriptionMaxLen {
		errs = append(errs, invalid("description", "max %d characters", noteDescriptionMaxLen))
	}

	return errors.Join(errs...)
}
