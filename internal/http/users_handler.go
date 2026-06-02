package http

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/RecceLog/api/internal/users"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/auth"
	"github.com/RecceLog/api/internal/http/dto"
	"github.com/RecceLog/api/internal/http/problem"
)

// handleGetMe returns the authenticated caller's own profile. Behind the auth
// middleware, so the user is always present in the context.
//
// @Summary     Get the current user
// @Description Returns the profile of the authenticated caller (the local user provisioned from the Keycloak token).
// @Tags        users
// @Produce     json
// @Success     200  {object}  dto.UserResponse
// @Failure     401  {object}  problem.Problem  "Missing or invalid token"
// @Failure     500  {object}  problem.Problem
// @Router      /v1/users/me [get]
func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFrom(r.Context())
	if !ok {
		problem.Unauthorized(w, r, "missing or malformed bearer token")
		return
	}
	writeJSON(w, http.StatusOK, dto.FromUser(*user))
}

// handleGetUser returns a user's public profile by id. Public — used to show
// the author of a route / note set and the author profile page.
//
// @Summary     Get a user's public profile
// @Description Returns the public profile (display name, description, avatar) of a user by id.
// @Tags        users
// @Produce     json
// @Param       id   path      string            true  "User ID (UUID)"
// @Success     200  {object}  dto.UserResponse
// @Failure     400  {object}  problem.Problem   "Invalid user id"
// @Failure     404  {object}  problem.Problem   "User not found"
// @Failure     500  {object}  problem.Problem
// @Router      /v1/users/{id} [get]
func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.BadRequest(w, r, "invalid user id")
		return
	}
	user, err := s.users.GetByID(r.Context(), id)
	if err != nil {
		problem.From(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.FromUser(user))
}

// handleGetProfilePic streams a user's profile picture. The application layer
// decides which file to serve (custom avatar or default) and its content type;
// the handler only resolves that to a path under the avatars directory and
// streams the bytes. The file name comes from the parsed UUID (never the raw
// URL), so it cannot escape the directory.
//
// @Summary     Get a user's profile picture
// @Description Returns the user's avatar image (custom if set, otherwise the default).
// @Tags        users
// @Produce     png
// @Produce     jpeg
// @Param       id   path  string  true  "User ID (UUID)"
// @Success     200  "Image bytes"
// @Failure     400  {object}  problem.Problem  "Invalid user id"
// @Failure     404  {object}  problem.Problem  "User not found"
// @Failure     500  {object}  problem.Problem
// @Router      /v1/users/{id}/profile_pic [get]
func (s *Server) handleGetProfilePic(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		problem.BadRequest(w, r, "invalid user id")
		return
	}

	name, contentType, err := s.users.ProfilePicture(r.Context(), id)
	if err != nil {
		problem.From(w, r, err)
		return
	}

	path := filepath.Join(s.avatarsDir, name)
	// If a custom avatar is recorded but its file is missing, fall back to the
	// default rather than 404-ing the image.
	if _, statErr := os.Stat(path); statErr != nil {
		path = filepath.Join(s.avatarsDir, users.DefaultAvatarFile)
		contentType = "image/png"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, path)
}
