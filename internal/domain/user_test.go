package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/RecceLog/api/internal/domain"
)

func TestUserValidate(t *testing.T) {
	tests := []struct {
		name       string
		user       domain.User
		wantErr    bool
		wantFields []string
	}{
		{
			name: "valid",
			user: domain.User{
				KeycloakSub: "keycloak|abc123",
				DisplayName: "Davide",
			},
		},
		{
			name:       "missing keycloak sub",
			user:       domain.User{DisplayName: "Davide"},
			wantErr:    true,
			wantFields: []string{"keycloakSub"},
		},
		{
			name:       "whitespace keycloak sub",
			user:       domain.User{KeycloakSub: "   ", DisplayName: "Davide"},
			wantErr:    true,
			wantFields: []string{"keycloakSub"},
		},
		{
			name:       "missing display name",
			user:       domain.User{KeycloakSub: "sub"},
			wantErr:    true,
			wantFields: []string{"displayName"},
		},
		{
			name: "display name too long",
			user: domain.User{
				KeycloakSub: "sub",
				DisplayName: strings.Repeat("a", 51),
			},
			wantErr:    true,
			wantFields: []string{"displayName"},
		},
		{
			name: "display name at limit",
			user: domain.User{
				KeycloakSub: "sub",
				DisplayName: strings.Repeat("a", 50),
			},
		},
		{
			name: "description too long",
			user: domain.User{
				KeycloakSub: "sub",
				DisplayName: "Davide",
				Description: strings.Repeat("a", 501),
			},
			wantErr:    true,
			wantFields: []string{"description"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.user.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("Validate() = nil, want error")
				}
				if !errors.Is(err, domain.ErrValidation) {
					t.Errorf("errors.Is(err, ErrValidation) = false")
				}
				assertFields(t, err, tt.wantFields)
				return
			}
			if err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}
