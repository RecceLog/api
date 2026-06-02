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
	// AvatarURL is the canonical, always-present URL of the user's profile
	// picture. It is derived from the id (not stored): the endpoint serves the
	// custom avatar when one exists, otherwise the default image.
	AvatarURL string    `json:"avatar_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FromUser maps a domain user to its public response shape. The avatar URL is
// computed from the id so the client never needs to know the storage layout.
func FromUser(u domain.User) UserResponse {
	return UserResponse{
		ID:          u.ID,
		DisplayName: u.DisplayName,
		Description: u.Description,
		AvatarURL:   "/v1/users/" + u.ID.String() + "/profile_pic",
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}
