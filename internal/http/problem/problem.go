package problem

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/RecceLog/api/internal/domain"
)

// Problem is the RFC 7807 problem details body.
// The optional Violations field is a domain-specific extension surfaced when
// a request fails one or more invariants — the standard explicitly allows
// member extensions on the base object.
type Problem struct {
	Type       string      `json:"type"`
	Title      string      `json:"title"`
	Status     int         `json:"status"`
	Detail     string      `json:"detail,omitempty"`
	Instance   string      `json:"instance,omitempty"`
	Violations []Violation `json:"violations,omitempty"`
}

// Violation describes a single failed invariant.
// Mirrors domain.ValidationError so server-side and client-side names line up.
type Violation struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

const mediaType = "application/problem+json"

// Write serializes p to w with the correct Content-Type and HTTP status.
func Write(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", mediaType)
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// From inspects err and emits the matching problem response.
// - domain.ErrNotFound          → 404
// - domain.ErrValidation chain  → 422 with violations harvested from the tree
// - anything else               → 500 (and the error is logged, not echoed)
func From(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		Write(w, Problem{
			Type:     "about:blank",
			Title:    "Not found",
			Status:   http.StatusNotFound,
			Instance: r.URL.Path,
		})
	case errors.Is(err, domain.ErrForbidden):
		Write(w, Problem{
			Type:     "about:blank",
			Title:    "Forbidden",
			Status:   http.StatusForbidden,
			Instance: r.URL.Path,
		})
	case errors.Is(err, domain.ErrValidation):
		Write(w, Problem{
			Type:       "about:blank",
			Title:      "Validation failed",
			Status:     http.StatusUnprocessableEntity,
			Detail:     err.Error(),
			Instance:   r.URL.Path,
			Violations: collectViolations(err),
		})
	default:
		// log full error server-side; never leak it to the client
		slog.ErrorContext(r.Context(), "unhandled error", "err", err, "path", r.URL.Path)
		Write(w, Problem{
			Type:     "about:blank",
			Title:    "Internal server error",
			Status:   http.StatusInternalServerError,
			Instance: r.URL.Path,
		})
	}
}

// Unauthorized emits a 401 — used when a request lacks a valid bearer token.
// The WWW-Authenticate header advertises the expected scheme per RFC 7235.
func Unauthorized(w http.ResponseWriter, r *http.Request, detail string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	Write(w, Problem{
		Type:     "about:blank",
		Title:    "Unauthorized",
		Status:   http.StatusUnauthorized,
		Detail:   detail,
		Instance: r.URL.Path,
	})
}

// BadRequest emits a 400 — used for malformed JSON, missing headers, bad UUIDs.
func BadRequest(w http.ResponseWriter, r *http.Request, detail string) {
	Write(w, Problem{
		Type:     "about:blank",
		Title:    "Bad request",
		Status:   http.StatusBadRequest,
		Detail:   detail,
		Instance: r.URL.Path,
	})
}

// collectViolations walks a (possibly errors.Join'd / fmt.Errorf-wrapped)
// tree and returns every *domain.ValidationError leaf, mapped to Violation.
func collectViolations(err error) []Violation {
	if err == nil {
		return nil
	}
	var out []Violation
	if v, ok := err.(*domain.ValidationError); ok {
		out = append(out, Violation{Field: v.Field, Message: v.Message})
	}
	switch e := err.(type) {
	case interface{ Unwrap() []error }:
		for _, sub := range e.Unwrap() {
			out = append(out, collectViolations(sub)...)
		}
	case interface{ Unwrap() error }:
		if len(out) == 0 {
			out = append(out, collectViolations(e.Unwrap())...)
		}
	}
	return out
}
