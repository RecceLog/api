// Package app is the application use-case layer. It orchestrates flows that
// span multiple aggregates (e.g. creating a route together with its first note
// set) and enforces authorization, so the HTTP handlers stay thin transport
// adapters: decode/validate the request shape, extract the caller identity,
// call one use case, map domain errors to status codes, write the response.
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
)

// Transactor runs a function inside a database transaction. Reentrant
// implementations let nested calls join the outer transaction. Declared here,
// on the consumer side, so the app layer never imports the storage package.
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// authorizeOwner enforces the "only the author may mutate" rule. lookupErr is
// the result of fetching the resource's author (e.g. domain.ErrNotFound when it
// is absent) and is returned as-is so a missing resource maps to 404. A NULL
// author (uuid.Nil, e.g. after the author was deleted) is owned by nobody, and
// a caller that is not the author gets domain.ErrForbidden (→ 403).
func authorizeOwner(callerID, author uuid.UUID, lookupErr error) error {
	if lookupErr != nil {
		return lookupErr
	}
	if callerID == uuid.Nil || author == uuid.Nil || author != callerID {
		return domain.ErrForbidden
	}
	return nil
}
