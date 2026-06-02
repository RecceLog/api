package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/auth"
	"github.com/RecceLog/api/internal/http/dto"
	"github.com/RecceLog/api/internal/http/problem"
)

// handleGetNotes returns the notes of a single note set.
// 404 if the set itself does not exist (distinguished from "empty set").
//
// @Summary     Get notes of a note set
// @Description Returns the notes belonging to the given set, ordered by their `order` field.
// @Tags        notes
// @Produce     json
// @Param       setID  path      string             true  "Note set ID (UUID)"
// @Success     200    {array}   dto.NoteResponse
// @Failure     400    {object}  problem.Problem    "Invalid note set id"
// @Failure     404    {object}  problem.Problem    "Note set not found"
// @Failure     500    {object}  problem.Problem
// @Router      /v1/notes/{setID} [get]
func (s *Server) handleGetNotes(w http.ResponseWriter, r *http.Request) {
	setID, err := uuid.Parse(chi.URLParam(r, "setID"))
	if err != nil {
		problem.BadRequest(w, r, "invalid note set id")
		return
	}
	notes, err := s.notes.GetNotes(r.Context(), setID)
	if err != nil {
		problem.From(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.FromNotes(notes))
}

// handleUpdateNote replaces the mutable fields of a single note. The note
// must belong to the set whose id is in the URL — a mismatch surfaces as
// 404 rather than silently editing the wrong note.
//
// @Summary     Update a single note
// @Description Replaces position, order, type, severity, direction and description of a note in a given set.
// @Tags        notes
// @Accept      json
// @Produce     json
// @Param       setID   path      string           true  "Note set ID (UUID)"
// @Param       noteID  path      string           true  "Note ID (UUID)"
// @Param       body    body      dto.NoteIn       true  "Updated note payload"
// @Success     200     {object}  dto.NoteResponse
// @Failure     400     {object}  problem.Problem  "Invalid IDs or malformed JSON"
// @Failure     403     {object}  problem.Problem  "Not the note set author"
// @Failure     404     {object}  problem.Problem  "Note not found (or not in this set)"
// @Failure     422     {object}  problem.Problem  "Validation failed"
// @Failure     500     {object}  problem.Problem
// @Router      /v1/notes/{setID}/{noteID} [patch]
func (s *Server) handleUpdateNote(w http.ResponseWriter, r *http.Request) {
	setID, err := uuid.Parse(chi.URLParam(r, "setID"))
	if err != nil {
		problem.BadRequest(w, r, "invalid note set id")
		return
	}
	noteID, err := uuid.Parse(chi.URLParam(r, "noteID"))
	if err != nil {
		problem.BadRequest(w, r, "invalid note id")
		return
	}

	var body dto.NoteIn
	if err := decodeJSON(w, r, &body); err != nil {
		problem.BadRequest(w, r, err.Error())
		return
	}

	note := body.ToDomain()
	note.ID = noteID

	// protected route: the auth middleware guarantees a user in context.
	user, _ := auth.UserFrom(r.Context())

	updated, err := s.notes.UpdateNote(r.Context(), user.ID, setID, note)
	if err != nil {
		problem.From(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.FromNote(updated))
}

// handleDeleteNoteSet removes a note set; CASCADE handles notes.
//
// @Summary     Delete a note set
// @Description Deletes the note set; ON DELETE CASCADE removes its notes.
// @Tags        notes
// @Param       setID  path  string  true  "Note set ID (UUID)"
// @Success     204    "No Content"
// @Failure     400    {object}  problem.Problem  "Invalid note set id"
// @Failure     403    {object}  problem.Problem  "Not the note set author"
// @Failure     404    {object}  problem.Problem  "Note set not found"
// @Failure     500    {object}  problem.Problem
// @Router      /v1/notes/{setID} [delete]
func (s *Server) handleDeleteNoteSet(w http.ResponseWriter, r *http.Request) {
	setID, err := uuid.Parse(chi.URLParam(r, "setID"))
	if err != nil {
		problem.BadRequest(w, r, "invalid note set id")
		return
	}
	// protected route: the auth middleware guarantees a user in context.
	user, _ := auth.UserFrom(r.Context())

	if err := s.notes.DeleteNoteSet(r.Context(), user.ID, setID); err != nil {
		problem.From(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteNote removes a single note. The note must belong to the set
// whose id is in the URL — a mismatch surfaces as 404 rather than silently
// deleting the wrong note.
//
// @Summary     Delete a single note
// @Description Deletes a note belonging to the given set. Returns 404 if the note does not exist or is not in this set.
// @Tags        notes
// @Param       setID   path  string  true  "Note set ID (UUID)"
// @Param       noteID  path  string  true  "Note ID (UUID)"
// @Success     204     "No Content"
// @Failure     400     {object}  problem.Problem  "Invalid IDs"
// @Failure     403     {object}  problem.Problem  "Not the note set author"
// @Failure     404     {object}  problem.Problem  "Note not found (or not in this set)"
// @Failure     500     {object}  problem.Problem
// @Router      /v1/notes/{setID}/{noteID} [delete]
func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	setID, err := uuid.Parse(chi.URLParam(r, "setID"))
	if err != nil {
		problem.BadRequest(w, r, "invalid note set id")
		return
	}
	noteID, err := uuid.Parse(chi.URLParam(r, "noteID"))
	if err != nil {
		problem.BadRequest(w, r, "invalid note id")
		return
	}

	// protected route: the auth middleware guarantees a user in context.
	user, _ := auth.UserFrom(r.Context())

	if err := s.notes.DeleteNote(r.Context(), user.ID, setID, noteID); err != nil {
		problem.From(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
