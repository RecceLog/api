package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RecceLog/api/internal/domain"
)

// Notes implements notes.Repository against PostgreSQL.
type Notes struct {
	pool *pgxpool.Pool
}

// NewNotes wires a Notes repository to the pool.
func NewNotes(pool *pgxpool.Pool) *Notes {
	return &Notes{pool: pool}
}

const createNoteSetSQL = `
INSERT INTO note_sets (route_id, author_id, name)
VALUES ($1, $2, NULLIF($3, ''))
RETURNING id, created_at, updated_at`

// CreateNoteSet inserts the note_set row only. Notes go in via AddNotes.
func (n *Notes) CreateNoteSet(ctx context.Context, ns domain.NoteSet) (domain.NoteSet, error) {
	q := querier(ctx, n.pool)

	err := q.QueryRow(ctx, createNoteSetSQL,
		ns.RouteID, nullableUUID(ns.AuthorID), ns.Name,
	).Scan(&ns.ID, &ns.CreatedAt, &ns.UpdatedAt)
	if err != nil {
		return domain.NoteSet{}, fmt.Errorf("insert note_set: %w", err)
	}
	return ns, nil
}

const addNotesSQL = `
INSERT INTO notes (id, set_id, position, "order", type, severity, direction, description)
SELECT t.id, $1,
       ST_SetSRID(ST_MakePoint(t.lng, t.lat), 4326),
       t.ord, t.type::note_type, NULLIF(t.severity, 0),
       NULLIF(t.direction, '')::direction_type, NULLIF(t.description, '')
FROM unnest(
    $2::uuid[], $3::float8[], $4::float8[], $5::int[],
    $6::text[], $7::int[], $8::text[], $9::text[]
) AS t(id, lng, lat, ord, type, severity, direction, description)`

// AddNotes batches all notes for a set into a single INSERT via unnest.
// IDs are generated client-side (uuid v7) so input order is preserved on
// return.
func (n *Notes) AddNotes(ctx context.Context, setID uuid.UUID, notes []domain.Note) ([]domain.Note, error) {
	if len(notes) == 0 {
		return nil, nil
	}
	q := querier(ctx, n.pool)

	ids := make([]uuid.UUID, len(notes))
	lngs := make([]float64, len(notes))
	lats := make([]float64, len(notes))
	orders := make([]int32, len(notes))
	types := make([]string, len(notes))
	severities := make([]int32, len(notes)) // 0 → NULL via NULLIF in SQL
	directions := make([]string, len(notes))
	descriptions := make([]string, len(notes))

	for i, note := range notes {
		if note.ID == uuid.Nil {
			id, err := uuid.NewV7()
			if err != nil {
				return nil, fmt.Errorf("gen note uuid: %w", err)
			}
			note.ID = id
		}
		ids[i] = note.ID
		lngs[i] = note.Position.Lng
		lats[i] = note.Position.Lat
		orders[i] = int32(note.Order)
		types[i] = string(note.Type)
		if note.Severity != nil {
			severities[i] = int32(*note.Severity)
		}
		directions[i] = string(note.Direction)
		descriptions[i] = note.Description
		notes[i] = note
	}

	if _, err := q.Exec(ctx, addNotesSQL,
		setID, ids, lngs, lats, orders, types, severities, directions, descriptions,
	); err != nil {
		return nil, fmt.Errorf("insert notes: %w", err)
	}
	return notes, nil
}

const listNoteSetsByRouteSQL = `
SELECT id, COALESCE(name, ''), route_id, author_id, created_at, updated_at
FROM note_sets
WHERE route_id = $1
ORDER BY created_at ASC`

const listNotesBySetsSQL = `
SELECT id, set_id, ST_X(position)::float8, ST_Y(position)::float8,
       "order", type::text, severity, COALESCE(direction::text, ''), COALESCE(description, '')
FROM notes
WHERE set_id = ANY($1::uuid[])
ORDER BY set_id, "order"`

// ListByRouteID returns every note set of a route with its notes pre-loaded.
// Two queries: one for note_sets, one for all notes across those sets — no
// N+1 regardless of the number of sets.
func (n *Notes) ListByRouteID(ctx context.Context, routeID uuid.UUID) ([]domain.NoteSet, error) {
	q := querier(ctx, n.pool)

	setRows, err := q.Query(ctx, listNoteSetsByRouteSQL, routeID)
	if err != nil {
		return nil, fmt.Errorf("select note_sets: %w", err)
	}
	defer setRows.Close()

	var sets []domain.NoteSet
	byID := make(map[uuid.UUID]int) // id → index in sets
	for setRows.Next() {
		var (
			s        domain.NoteSet
			authorID pgtype.UUID
		)
		if err := setRows.Scan(&s.ID, &s.Name, &s.RouteID, &authorID, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan note_set: %w", err)
		}
		if authorID.Valid {
			s.AuthorID = uuid.UUID(authorID.Bytes)
		}
		byID[s.ID] = len(sets)
		sets = append(sets, s)
	}
	if err := setRows.Err(); err != nil {
		return nil, err
	}
	if len(sets) == 0 {
		return nil, nil
	}

	setIDs := make([]uuid.UUID, 0, len(sets))
	for id := range byID {
		setIDs = append(setIDs, id)
	}

	noteRows, err := q.Query(ctx, listNotesBySetsSQL, setIDs)
	if err != nil {
		return nil, fmt.Errorf("select notes: %w", err)
	}
	defer noteRows.Close()

	for noteRows.Next() {
		var (
			note     domain.Note
			setID    uuid.UUID
			order    int32
			severity pgtype.Int4
			typ      string
			dir      string
		)
		if err := noteRows.Scan(
			&note.ID, &setID, &note.Position.Lng, &note.Position.Lat,
			&order, &typ, &severity, &dir, &note.Description,
		); err != nil {
			return nil, fmt.Errorf("scan note: %w", err)
		}
		note.Order = int(order)
		note.Type = domain.NoteType(typ)
		note.Direction = domain.DirectionType(dir)
		if severity.Valid {
			sev := int(severity.Int32)
			note.Severity = &sev
		}
		idx, ok := byID[setID]
		if !ok {
			continue // shouldn't happen given the WHERE clause
		}
		sets[idx].Notes = append(sets[idx].Notes, note)
	}
	return sets, noteRows.Err()
}

const listNotesBySetSQL = `
SELECT id, ST_X(position)::float8, ST_Y(position)::float8,
       "order", type::text, severity, COALESCE(direction::text, ''), COALESCE(description, '')
FROM notes
WHERE set_id = $1
ORDER BY "order"`

// ListBySetID returns the notes of a specific set.
// Returns domain.ErrNotFound if the set itself doesn't exist.
func (n *Notes) ListBySetID(ctx context.Context, setID uuid.UUID) ([]domain.Note, error) {
	q := querier(ctx, n.pool)

	// existence probe — distinguishes empty set from missing set.
	var exists bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM note_sets WHERE id = $1)`, setID).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("check note_set exists: %w", err)
	}
	if !exists {
		return nil, domain.ErrNotFound
	}

	rows, err := q.Query(ctx, listNotesBySetSQL, setID)
	if err != nil {
		return nil, fmt.Errorf("select notes: %w", err)
	}
	defer rows.Close()

	var out []domain.Note
	for rows.Next() {
		var (
			note     domain.Note
			order    int32
			severity pgtype.Int4
			typ      string
			dir      string
		)
		if err := rows.Scan(
			&note.ID, &note.Position.Lng, &note.Position.Lat,
			&order, &typ, &severity, &dir, &note.Description,
		); err != nil {
			return nil, fmt.Errorf("scan note: %w", err)
		}
		note.Order = int(order)
		note.Type = domain.NoteType(typ)
		note.Direction = domain.DirectionType(dir)
		if severity.Valid {
			sev := int(severity.Int32)
			note.Severity = &sev
		}
		out = append(out, note)
	}
	return out, rows.Err()
}

const getNoteSetAuthorSQL = `SELECT author_id FROM note_sets WHERE id = $1`

// GetSetAuthor returns the note set's author id, or uuid.Nil when NULL.
// ErrNotFound when the set is absent.
func (n *Notes) GetSetAuthor(ctx context.Context, setID uuid.UUID) (uuid.UUID, error) {
	q := querier(ctx, n.pool)

	var authorID pgtype.UUID
	err := q.QueryRow(ctx, getNoteSetAuthorSQL, setID).Scan(&authorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("select note_set author: %w", err)
	}
	if !authorID.Valid {
		return uuid.Nil, nil
	}
	return uuid.UUID(authorID.Bytes), nil
}

const updateNoteSQL = `
UPDATE notes SET
    position    = ST_SetSRID(ST_MakePoint($3, $4), 4326),
    "order"     = $5,
    type        = $6::note_type,
    severity    = NULLIF($7, 0),
    direction   = NULLIF($8, '')::direction_type,
    description = NULLIF($9, '')
WHERE id = $1 AND set_id = $2
RETURNING id`

// UpdateNote replaces the mutable fields of a single note. The set_id guard
// in the WHERE clause prevents editing a note via the wrong parent URL.
func (n *Notes) UpdateNote(ctx context.Context, setID uuid.UUID, note domain.Note) (domain.Note, error) {
	q := querier(ctx, n.pool)

	severity := int32(0)
	if note.Severity != nil {
		severity = int32(*note.Severity)
	}

	var returnedID uuid.UUID
	err := q.QueryRow(ctx, updateNoteSQL,
		note.ID, setID,
		note.Position.Lng, note.Position.Lat,
		int32(note.Order),
		string(note.Type), severity, string(note.Direction), note.Description,
	).Scan(&returnedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Note{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Note{}, fmt.Errorf("update note: %w", err)
	}
	return note, nil
}

const deleteNoteSetSQL = `DELETE FROM note_sets WHERE id = $1`

// DeleteNoteSet removes a note set; CASCADE reaps its notes.
func (n *Notes) DeleteNoteSet(ctx context.Context, setID uuid.UUID) error {
	q := querier(ctx, n.pool)
	tag, err := q.Exec(ctx, deleteNoteSetSQL, setID)
	if err != nil {
		return fmt.Errorf("delete note_set: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

const deleteNoteSQL = `DELETE FROM notes WHERE id = $2 AND set_id = $1`

// DeleteNote removes a single note. The set_id guard prevents deleting a note
// via the wrong parent URL; a missing row surfaces as ErrNotFound.
func (n *Notes) DeleteNote(ctx context.Context, setID, noteID uuid.UUID) error {
	q := querier(ctx, n.pool)
	tag, err := q.Exec(ctx, deleteNoteSQL, setID, noteID)
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

