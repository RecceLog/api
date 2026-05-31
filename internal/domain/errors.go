package domain

import (
	"errors"
	"fmt"
)

// Domain sentinel errors. The outer layers recognize them via `errors.Is`
// and map them to their own domain (storage -> no row, http -> 404 / 422)
// without the domain knowing anything about SQL or HTTP.
var (
	// ErrNotFound when the requested resource is not found.
	ErrNotFound = errors.New("resource not found")

	// ErrValidation is a marker for any violation of domain invariants.
	// *ValidationErrors unwrap to this error, so `errors.Is(err, ErrValidation)`
	// remains true even within an `errors.Join`
	ErrValidation = errors.New("invalid data")

	// ErrForbidden when the caller is authenticated but not allowed to act on
	// the resource (e.g. editing content they did not author). Outer layers map
	// it to HTTP 403.
	ErrForbidden = errors.New("forbidden")
)

// ValidationError a single invalid field. Implements error and
// Unwrap towards ErrValidation; this means it can be used with both
// errors.Is (for mapping to 422) and errors.As (to extract the HTTP Field/Message).
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

func (e *ValidationError) Unwrap() error {
	return ErrValidation
}

// invalid builds a *ValidationError. Internal package utility.
// The message is formatted with fmt.Sprintf when args are provided.
func invalid(field, message string, args ...any) error {
	if len(args) > 0 {
		message = fmt.Sprintf(message, args...)
	}
	return &ValidationError{Field: field, Message: message}
}

// wrapIndex prefixes an element’s error with its position in the collection
// preserving the Unwrap chain (i.e., errors.Is(.., ErrValidation)).
func wrapIndex(collection string, i int, err error) error {
	return fmt.Errorf("%s[%d]: %w", collection, i, err)
}
