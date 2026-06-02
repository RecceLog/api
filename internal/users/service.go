package users

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
)

// Service owns the application logic for the User aggregate. Authentication is
// Keycloak's job; this service only mirrors the parts of an identity the app
// needs (display name, profile) into the local database.
type Service struct {
	repo Repository
}

// NewService wires a Service to its Repository.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// EnsureProvisioned returns the local user federated under keycloakSub,
// creating it on first sight (just-in-time provisioning). This is the hook the
// auth middleware calls on every authenticated request: once Keycloak has
// vouched for the caller, we guarantee a matching local row exists so data can
// be attributed to them.
//
// displayName is taken from the token claims; when empty it falls back to the
// subject so the domain invariant (non-empty display name) always holds.
func (s *Service) EnsureProvisioned(ctx context.Context, keycloakSub, displayName string) (domain.User, error) {
	u, err := s.repo.GetByKeycloakSub(ctx, keycloakSub)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, err
	}

	newUser := domain.User{KeycloakSub: keycloakSub, DisplayName: displayName}
	if newUser.DisplayName == "" {
		newUser.DisplayName = keycloakSub
	}
	if err := newUser.Validate(); err != nil {
		return domain.User{}, err
	}
	return s.repo.Create(ctx, newUser)
}

// GetByID returns a user's profile by local id. Returns domain.ErrNotFound
// when absent. Backs the public profile endpoint.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	return s.repo.GetByID(ctx, id)
}

// DefaultAvatarFile is the avatar served for a user with no custom picture.
const DefaultAvatarFile = "default.png"

// ProfilePicture resolves which avatar to serve for a user: the file name and
// its content type. A user with a custom avatar maps to "<id>.<ext>" with the
// stored content type; otherwise to the default image. It only decides the
// logical file — turning that into a path on disk and streaming the bytes is
// the HTTP layer's job. Returns domain.ErrNotFound if the user does not exist.
func (s *Service) ProfilePicture(ctx context.Context, id uuid.UUID) (name, contentType string, err error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return "", "", err
	}
	switch user.AvatarContentType {
	case "image/png":
		return id.String() + ".png", "image/png", nil
	case "image/jpeg":
		return id.String() + ".jpg", "image/jpeg", nil
	case "image/webp":
		return id.String() + ".webp", "image/webp", nil
	default:
		return DefaultAvatarFile, "image/png", nil
	}
}
