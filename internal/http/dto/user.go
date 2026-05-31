package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/domain"
)

// UserResponse is the public profile shape of a user. It deliberately omits
// the Keycloak subject (an IdP-internal identifier) — clients only need the
// app-facing profile.
type UserResponse struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Description string    `json:"description,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// FromUser maps a domain user to its public response shape.
func FromUser(u domain.User) UserResponse {
	return UserResponse{
		ID:          u.ID,
		DisplayName: u.DisplayName,
		Description: u.Description,
		AvatarURL:   u.AvatarURL,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}
