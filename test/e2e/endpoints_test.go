//go:build integration

package e2e

import (
	"net/http"
	"testing"
)

// ----- note sets ------------------------------------------------------------

func TestAddNoteSet(t *testing.T) {
	route := createRoute(t, "author")
	routeID := route["id"].(string)
	body := map[string]any{
		"name":  "Extra",
		"notes": []map[string]any{{"order": 1, "type": "WARNING", "position": map[string]float64{"lng": 9.19, "lat": 45.46}}},
	}

	t.Run("no token", func(t *testing.T) {
		r := req(t, http.MethodPost, "/v1/routes/"+routeID+"/notes", "", body, nil)
		assertStatus(t, r, http.StatusUnauthorized)
	})
	t.Run("any user may annotate another user's route", func(t *testing.T) {
		// "stranger" is NOT the route author — the API allows this by design.
		r := req(t, http.MethodPost, "/v1/routes/"+routeID+"/notes", "stranger", body, nil)
		assertStatus(t, r, http.StatusCreated)
	})
	t.Run("no notes", func(t *testing.T) {
		r := req(t, http.MethodPost, "/v1/routes/"+routeID+"/notes", "author", `{"notes":[]}`, nil)
		assertStatus(t, r, http.StatusUnprocessableEntity)
	})
	t.Run("nonexistent route", func(t *testing.T) {
		r := req(t, http.MethodPost, "/v1/routes/019e8012-0000-7000-8000-000000000000/notes", "author", body, nil)
		assertStatus(t, r, http.StatusNotFound)
	})
}

// ----- waypoints ------------------------------------------------------------

func TestWaypoints(t *testing.T) {
	route := createRoute(t, "wp-owner")
	routeID := route["id"].(string)
	wp := map[string]any{"name": "CP", "order": 1, "position": map[string]float64{"lng": 11.0, "lat": 44.0}}

	t.Run("no token", func(t *testing.T) {
		r := req(t, http.MethodPost, "/v1/routes/"+routeID+"/waypoints", "", wp, nil)
		assertStatus(t, r, http.StatusUnauthorized)
	})
	t.Run("not the author", func(t *testing.T) {
		r := req(t, http.MethodPost, "/v1/routes/"+routeID+"/waypoints", "intruder", wp, nil)
		assertStatus(t, r, http.StatusForbidden)
	})
	t.Run("nonexistent route", func(t *testing.T) {
		r := req(t, http.MethodPost, "/v1/routes/019e8012-0000-7000-8000-000000000000/waypoints", "wp-owner", wp, nil)
		assertStatus(t, r, http.StatusNotFound)
	})

	// author adds, updates, deletes
	created := req(t, http.MethodPost, "/v1/routes/"+routeID+"/waypoints", "wp-owner", wp, nil)
	assertStatus(t, created, http.StatusCreated)
	var saved map[string]any
	created.decode(t, &saved)
	wpID := saved["id"].(string)

	t.Run("update", func(t *testing.T) {
		upd := map[string]any{"name": "CP2", "order": 1, "position": map[string]float64{"lng": 11.1, "lat": 44.1}}
		r := req(t, http.MethodPatch, "/v1/routes/"+routeID+"/waypoints/"+wpID, "wp-owner", upd, nil)
		assertStatus(t, r, http.StatusOK)
	})
	t.Run("update foreign waypoint id → 404", func(t *testing.T) {
		upd := map[string]any{"name": "x", "order": 1, "position": map[string]float64{"lng": 11.1, "lat": 44.1}}
		r := req(t, http.MethodPatch, "/v1/routes/"+routeID+"/waypoints/019e8012-0000-7000-8000-000000000000", "wp-owner", upd, nil)
		assertStatus(t, r, http.StatusNotFound)
	})
	t.Run("delete", func(t *testing.T) {
		r := req(t, http.MethodDelete, "/v1/routes/"+routeID+"/waypoints/"+wpID, "wp-owner", nil, nil)
		assertStatus(t, r, http.StatusNoContent)
	})
}

// ----- notes ----------------------------------------------------------------

// firstSetAndNote returns the first note set id and its first note id from a
// route-detail response.
func firstSetAndNote(t *testing.T, route map[string]any) (setID, noteID string) {
	t.Helper()
	sets := route["note_sets"].([]any)
	set := sets[0].(map[string]any)
	notes := set["notes"].([]any)
	note := notes[0].(map[string]any)
	return set["id"].(string), note["id"].(string)
}

func TestNotes(t *testing.T) {
	route := createRoute(t, "note-owner")
	setID, noteID := firstSetAndNote(t, route)

	t.Run("get notes", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/notes/"+setID, "", nil, nil)
		assertStatus(t, r, http.StatusOK)
	})
	t.Run("get notes invalid id", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/notes/not-a-uuid", "", nil, nil)
		assertStatus(t, r, http.StatusBadRequest)
	})
	t.Run("get notes of missing set", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/notes/019e8012-0000-7000-8000-000000000000", "", nil, nil)
		assertStatus(t, r, http.StatusNotFound)
	})

	update := map[string]any{"order": 1, "type": "WARNING", "description": "Updated", "position": map[string]float64{"lng": 9.19, "lat": 45.46}}

	t.Run("author updates note", func(t *testing.T) {
		r := req(t, http.MethodPatch, "/v1/notes/"+setID+"/"+noteID, "note-owner", update, nil)
		assertStatus(t, r, http.StatusOK)
	})
	t.Run("non-author forbidden", func(t *testing.T) {
		r := req(t, http.MethodPatch, "/v1/notes/"+setID+"/"+noteID, "intruder", update, nil)
		assertStatus(t, r, http.StatusForbidden)
	})
	t.Run("severity rule violated", func(t *testing.T) {
		bad := map[string]any{"order": 1, "type": "INDICATION", "direction": "LEFT", "position": map[string]float64{"lng": 9.19, "lat": 45.46}}
		r := req(t, http.MethodPatch, "/v1/notes/"+setID+"/"+noteID, "note-owner", bad, nil)
		assertStatus(t, r, http.StatusUnprocessableEntity)
	})
	t.Run("foreign note id → 404", func(t *testing.T) {
		r := req(t, http.MethodPatch, "/v1/notes/"+setID+"/019e8012-0000-7000-8000-000000000000", "note-owner", update, nil)
		assertStatus(t, r, http.StatusNotFound)
	})
	t.Run("delete note", func(t *testing.T) {
		r := req(t, http.MethodDelete, "/v1/notes/"+setID+"/"+noteID, "note-owner", nil, nil)
		assertStatus(t, r, http.StatusNoContent)
	})
	t.Run("non-author cannot delete set", func(t *testing.T) {
		r := req(t, http.MethodDelete, "/v1/notes/"+setID, "intruder", nil, nil)
		assertStatus(t, r, http.StatusForbidden)
	})
	t.Run("author deletes set", func(t *testing.T) {
		r := req(t, http.MethodDelete, "/v1/notes/"+setID, "note-owner", nil, nil)
		assertStatus(t, r, http.StatusNoContent)
	})
}

// ----- users ----------------------------------------------------------------

func TestUsers(t *testing.T) {
	t.Run("me without token", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/users/me", "", nil, nil)
		assertStatus(t, r, http.StatusUnauthorized)
	})

	// authenticate as "alice" → provisions her and returns her local profile.
	meResp := req(t, http.MethodGet, "/v1/users/me", "alice", nil, nil)
	assertStatus(t, meResp, http.StatusOK)
	var me map[string]any
	meResp.decode(t, &me)
	userID := me["id"].(string)

	if me["avatar_url"] != "/v1/users/"+userID+"/profile_pic" {
		t.Errorf("avatar_url = %v, want derived profile_pic path", me["avatar_url"])
	}

	t.Run("public profile", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/users/"+userID, "", nil, nil)
		assertStatus(t, r, http.StatusOK)
	})
	t.Run("invalid id", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/users/not-a-uuid", "", nil, nil)
		assertStatus(t, r, http.StatusBadRequest)
	})
	t.Run("nonexistent", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/users/019e8012-0000-7000-8000-000000000000", "", nil, nil)
		assertStatus(t, r, http.StatusNotFound)
	})
	t.Run("default profile picture", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/users/"+userID+"/profile_pic", "", nil, nil)
		assertStatus(t, r, http.StatusOK)
	})
}

// TestNoteProximity covers the "a note must be within 50 m of the route path"
// rule (enforced by a PostGIS trigger, surfaced as 422).
func TestNoteProximity(t *testing.T) {
	// A note far from the path (lat/lng 0,0 — off the African coast) is rejected.
	farNote := map[string]any{"order": 1, "type": "WARNING", "position": map[string]float64{"lng": 0, "lat": 0}}

	t.Run("rejected at route creation", func(t *testing.T) {
		body := validCreateBody()
		body["note_set"].(map[string]any)["notes"] = []map[string]any{farNote}
		r := req(t, http.MethodPost, "/v1/routes", "prox", body, nil)
		assertStatus(t, r, http.StatusUnprocessableEntity)
	})

	route := createRoute(t, "prox")
	routeID := route["id"].(string)

	t.Run("rejected when adding a note set", func(t *testing.T) {
		r := req(t, http.MethodPost, "/v1/routes/"+routeID+"/notes", "prox",
			map[string]any{"notes": []map[string]any{farNote}}, nil)
		assertStatus(t, r, http.StatusUnprocessableEntity)
	})

	t.Run("rejected when updating a note far off-path", func(t *testing.T) {
		setID, noteID := firstSetAndNote(t, route)
		bad := map[string]any{"order": 1, "type": "WARNING", "position": map[string]float64{"lng": 0, "lat": 0}}
		r := req(t, http.MethodPatch, "/v1/notes/"+setID+"/"+noteID, "prox", bad, nil)
		assertStatus(t, r, http.StatusUnprocessableEntity)
	})

	t.Run("a note on the path is accepted", func(t *testing.T) {
		// 9.19,45.46 is the first path vertex → distance 0.
		r := req(t, http.MethodPost, "/v1/routes/"+routeID+"/notes", "prox",
			map[string]any{"notes": []map[string]any{{"order": 1, "type": "WARNING", "position": map[string]float64{"lng": 9.19, "lat": 45.46}}}}, nil)
		assertStatus(t, r, http.StatusCreated)
	})
}

func TestHealth(t *testing.T) {
	r := req(t, http.MethodGet, "/health", "", nil, nil)
	assertStatus(t, r, http.StatusOK)
}
