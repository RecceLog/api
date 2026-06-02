package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RecceLog/api/internal/domain"
)

// Users implements users.Repository against PostgreSQL.
type Users struct {
	pool *pgxpool.Pool
}

// NewUsers wires a Users repository to the pool.
func NewUsers(pool *pgxpool.Pool) *Users {
	return &Users{pool: pool}
}

const getUserByKeycloakSubSQL = `
SELECT id, keycloak_sub, display_name,
       COALESCE(description, ''), COALESCE(avatar_content_type, ''),
       created_at, updated_at
FROM users
WHERE keycloak_sub = $1`

// GetByKeycloakSub returns the user for a Keycloak subject, or ErrNotFound.
func (u *Users) GetByKeycloakSub(ctx context.Context, keycloakSub string) (domain.User, error) {
	q := querier(ctx, u.pool)

	var out domain.User
	err := q.QueryRow(ctx, getUserByKeycloakSubSQL, keycloakSub).Scan(
		&out.ID, &out.KeycloakSub, &out.DisplayName,
		&out.Description, &out.AvatarContentType,
		&out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("select user: %w", err)
	}
	return out, nil
}

const getUserByIDSQL = `
SELECT id, keycloak_sub, display_name,
       COALESCE(description, ''), COALESCE(avatar_content_type, ''),
       created_at, updated_at
FROM users
WHERE id = $1`

// GetByID returns a user by local id, or ErrNotFound.
func (u *Users) GetByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	q := querier(ctx, u.pool)

	var out domain.User
	err := q.QueryRow(ctx, getUserByIDSQL, id).Scan(
		&out.ID, &out.KeycloakSub, &out.DisplayName,
		&out.Description, &out.AvatarContentType,
		&out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("select user by id: %w", err)
	}
	return out, nil
}

// createUserSQL is upsert-shaped on purpose: two concurrent first requests for
// the same Keycloak subject would otherwise race on the UNIQUE constraint. The
// no-op DO UPDATE (re-assigning keycloak_sub to itself) makes RETURNING yield
// the row whether it was just inserted or already existed.
const createUserSQL = `
INSERT INTO users (keycloak_sub, display_name, description)
VALUES ($1, $2, NULLIF($3, ''))
ON CONFLICT (keycloak_sub) DO UPDATE SET keycloak_sub = EXCLUDED.keycloak_sub
RETURNING id, keycloak_sub, display_name,
          COALESCE(description, ''), COALESCE(avatar_content_type, ''),
          created_at, updated_at`

// Create inserts a user (idempotent on keycloak_sub). A freshly provisioned
// user has no custom avatar (avatar_content_type stays NULL).
func (u *Users) Create(ctx context.Context, user domain.User) (domain.User, error) {
	q := querier(ctx, u.pool)

	var out domain.User
	err := q.QueryRow(ctx, createUserSQL,
		user.KeycloakSub, user.DisplayName, user.Description,
	).Scan(
		&out.ID, &out.KeycloakSub, &out.DisplayName,
		&out.Description, &out.AvatarContentType,
		&out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return domain.User{}, fmt.Errorf("insert user: %w", err)
	}
	return out, nil
}
