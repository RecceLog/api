package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	userDisplayNameMaxLen = 50
	userDescriptionMaxLen = 500
)

// User is the application user, federated through Keycloak.
// The KeycloakSub is the stable subject identifier issued by the IdP and is
// what links a local record to the external identity.
// A user must have:
//   - a Keycloak subject
//   - a display name
type User struct {
	ID           uuid.UUID
	KeycloakSub  string
	DisplayName  string
	Description  string
	AvatarURL    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Validate checks that user values respect the domain invariants.
// It gathers eventual errors with `errors.Join`.
// Returns nil if no errors are found.
func (u User) Validate() error {
	var errs []error

	if strings.TrimSpace(u.KeycloakSub) == "" {
		errs = append(errs, invalid("keycloakSub", "can't be empty"))
	}
	if strings.TrimSpace(u.DisplayName) == "" {
		errs = append(errs, invalid("displayName", "can't be empty"))
	}
	if utf8.RuneCountInString(u.DisplayName) > userDisplayNameMaxLen {
		errs = append(errs, invalid("displayName", "max %d characters", userDisplayNameMaxLen))
	}
	if utf8.RuneCountInString(u.Description) > userDescriptionMaxLen {
		errs = append(errs, invalid("description", "max %d characters", userDescriptionMaxLen))
	}

	return errors.Join(errs...)
}
