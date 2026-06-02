package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
)

// UserService is the use-case layer for user profiles.
type UserService struct {
	users UserAggregate
}

// NewUserService wires the user use cases to the user aggregate service.
func NewUserService(usersSvc UserAggregate) *UserService {
	return &UserService{users: usersSvc}
}

// GetByID returns a user's public profile. Returns domain.ErrNotFound if the
// user does not exist.
func (s *UserService) GetByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	return s.users.GetByID(ctx, id)
}

// ProfilePicture resolves the file name and content type of a user's avatar.
// Returns domain.ErrNotFound if the user does not exist.
func (s *UserService) ProfilePicture(ctx context.Context, id uuid.UUID) (name, contentType string, err error) {
	return s.users.ProfilePicture(ctx, id)
}
