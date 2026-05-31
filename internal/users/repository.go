package users

import (
	"context"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
)

// Repository persists and retrieves application users. The DB holds only
// app-facing profile data — credentials, tokens and everything security-
// related live in Keycloak. The link between the two is the KeycloakSub.
//
// The interface is declared here, on the consumer side, so the storage layer
// adapts to it without leaking driver concerns into the service.
type Repository interface {
	// GetByKeycloakSub returns the user federated under the given Keycloak
	// subject. Returns domain.ErrNotFound when no local record exists yet
	// (the just-in-time provisioning path then creates it).
	GetByKeycloakSub(ctx context.Context, keycloakSub string) (domain.User, error)

	// GetByID returns the user by local id. Returns domain.ErrNotFound when
	// absent. Backs the public profile endpoint.
	GetByID(ctx context.Context, id uuid.UUID) (domain.User, error)

	// Create inserts a user. The insert is idempotent on keycloak_sub: a
	// concurrent first request for the same subject returns the already-stored
	// row instead of failing on the UNIQUE constraint.
	Create(ctx context.Context, u domain.User) (domain.User, error)
}
