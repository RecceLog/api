//go:build integration

package e2e

import (
	"net/http"
	"strings"
	"testing"
)

func TestCreateRouteValidation(t *testing.T) {
	const tok = "creator"

	cases := []struct {
		name string
		body any // string = raw JSON; map = encoded
		want int
	}{
		{"no token", nil, http.StatusUnauthorized}, // handled separately below
		{"no body", nil, http.StatusBadRequest},
		{"empty body", "", http.StatusBadRequest},
		{"empty object", "{}", http.StatusUnprocessableEntity},
		{"unknown field", `{"route":{"name":"R","vehicles":["CAR"],"path":[{"lng":9.19,"lat":45.46},{"lng":12.49,"lat":41.9}]},"note_set":{"notes":[{"order":1,"type":"WARNING"}]},"surprise":true}`, http.StatusBadRequest},
		{"only route, no note set", `{"route":{"name":"R","vehicles":["CAR"],"path":[{"lng":9.19,"lat":45.46},{"lng":12.49,"lat":41.9}]}}`, http.StatusUnprocessableEntity},
		{"only note set, no route", `{"note_set":{"notes":[{"order":1,"type":"WARNING"}]}}`, http.StatusUnprocessableEntity},
		{"note set without notes", `{"route":{"name":"R","vehicles":["CAR"],"path":[{"lng":9.19,"lat":45.46},{"lng":12.49,"lat":41.9}]},"note_set":{"notes":[]}}`, http.StatusUnprocessableEntity},
		{"single-point path", `{"route":{"name":"R","vehicles":["CAR"],"path":[{"lng":9.19,"lat":45.46}]},"note_set":{"notes":[{"order":1,"type":"WARNING"}]}}`, http.StatusUnprocessableEntity},
		{"invalid vehicle", `{"route":{"name":"R","vehicles":["PLANE"],"path":[{"lng":9.19,"lat":45.46},{"lng":12.49,"lat":41.9}]},"note_set":{"notes":[{"order":1,"type":"WARNING"}]}}`, http.StatusUnprocessableEntity},
		{"duplicate vehicles", `{"route":{"name":"R","vehicles":["CAR","CAR"],"path":[{"lng":9.19,"lat":45.46},{"lng":12.49,"lat":41.9}]},"note_set":{"notes":[{"order":1,"type":"WARNING"}]}}`, http.StatusUnprocessableEntity},
		{"too many vehicles", `{"route":{"name":"R","vehicles":["FOOT","BIKE","MOTORCYCLE","CAR","FOOT"],"path":[{"lng":9.19,"lat":45.46},{"lng":12.49,"lat":41.9}]},"note_set":{"notes":[{"order":1,"type":"WARNING"}]}}`, http.StatusUnprocessableEntity},
		{"indication without direction", `{"route":{"name":"R","vehicles":["CAR"],"path":[{"lng":9.19,"lat":45.46},{"lng":12.49,"lat":41.9}]},"note_set":{"notes":[{"order":1,"type":"INDICATION","position":{"lng":9.19,"lat":45.46}}]}}`, http.StatusUnprocessableEntity},
		{"non-straight indication without severity", `{"route":{"name":"R","vehicles":["CAR"],"path":[{"lng":9.19,"lat":45.46},{"lng":12.49,"lat":41.9}]},"note_set":{"notes":[{"order":1,"type":"INDICATION","direction":"LEFT","position":{"lng":9.19,"lat":45.46}}]}}`, http.StatusUnprocessableEntity},
		{"severity out of range", `{"route":{"name":"R","vehicles":["CAR"],"path":[{"lng":9.19,"lat":45.46},{"lng":12.49,"lat":41.9}]},"note_set":{"notes":[{"order":1,"type":"WARNING","severity":8,"position":{"lng":9.19,"lat":45.46}}]}}`, http.StatusUnprocessableEntity},
		{"name too long", `{"route":{"name":"` + strings.Repeat("A", 121) + `","vehicles":["CAR"],"path":[{"lng":9.19,"lat":45.46},{"lng":12.49,"lat":41.9}]},"note_set":{"notes":[{"order":1,"type":"WARNING"}]}}`, http.StatusUnprocessableEntity},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			token := tok
			if c.name == "no token" {
				token = "" // unauthenticated
			}
			r := req(t, http.MethodPost, "/v1/routes", token, c.body, nil)
			assertStatus(t, r, c.want)
		})
	}
}

func TestCreateRouteValid(t *testing.T) {
	route := createRoute(t, "creator")

	if route["id"] == nil || route["id"] == "" {
		t.Fatalf("missing id in response: %v", route)
	}
	// geocoder is nil in tests → cities fall back to the placeholder.
	if route["start_city"] != "N/A" || route["finish_city"] != "N/A" {
		t.Errorf("cities = %v / %v, want N/A placeholders", route["start_city"], route["finish_city"])
	}
	sets, ok := route["note_sets"].([]any)
	if !ok || len(sets) != 1 {
		t.Fatalf("expected exactly one note set, got %v", route["note_sets"])
	}
}

func TestGetRoute(t *testing.T) {
	created := createRoute(t, "reader")
	id := created["id"].(string)

	t.Run("found", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/routes/"+id, "", nil, nil)
		assertStatus(t, r, http.StatusOK)
	})
	t.Run("invalid id", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/routes/not-a-uuid", "", nil, nil)
		assertStatus(t, r, http.StatusBadRequest)
	})
	t.Run("not found", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/routes/019e8012-0000-7000-8000-000000000000", "", nil, nil)
		assertStatus(t, r, http.StatusNotFound)
	})
}

func TestListRoutes(t *testing.T) {
	createRoute(t, "lister")
	r := req(t, http.MethodGet, "/v1/routes", "", nil, nil)
	assertStatus(t, r, http.StatusOK)
	var list []map[string]any
	r.decode(t, &list)
	if len(list) == 0 {
		t.Fatal("expected at least one route")
	}
}

func TestNearbySearch(t *testing.T) {
	createRoute(t, "nearby")
	center := map[string]string{"Latitude": "45.46", "Longitude": "9.19"}

	t.Run("valid", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/routes/range/100000", "", nil, center)
		assertStatus(t, r, http.StatusOK)
	})
	t.Run("missing headers", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/routes/range/10000", "", nil, nil)
		assertStatus(t, r, http.StatusBadRequest)
	})
	t.Run("invalid radius", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/routes/range/abc", "", nil, center)
		assertStatus(t, r, http.StatusBadRequest)
	})
	t.Run("radius too large", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/routes/range/600000", "", nil, center)
		assertStatus(t, r, http.StatusUnprocessableEntity)
	})
	t.Run("invalid vehicle filter", func(t *testing.T) {
		r := req(t, http.MethodGet, "/v1/routes/range/10000?vehicle=PLANE", "", nil, center)
		assertStatus(t, r, http.StatusUnprocessableEntity)
	})
}

func TestDeleteRoute(t *testing.T) {
	created := createRoute(t, "owner")
	id := created["id"].(string)

	t.Run("no token", func(t *testing.T) {
		r := req(t, http.MethodDelete, "/v1/routes/"+id, "", nil, nil)
		assertStatus(t, r, http.StatusUnauthorized)
	})
	t.Run("not the author", func(t *testing.T) {
		r := req(t, http.MethodDelete, "/v1/routes/"+id, "intruder", nil, nil)
		assertStatus(t, r, http.StatusForbidden)
	})
	t.Run("nonexistent", func(t *testing.T) {
		r := req(t, http.MethodDelete, "/v1/routes/019e8012-0000-7000-8000-000000000000", "owner", nil, nil)
		assertStatus(t, r, http.StatusNotFound)
	})
	t.Run("author deletes", func(t *testing.T) {
		r := req(t, http.MethodDelete, "/v1/routes/"+id, "owner", nil, nil)
		assertStatus(t, r, http.StatusNoContent)
		// gone now
		g := req(t, http.MethodGet, "/v1/routes/"+id, "", nil, nil)
		assertStatus(t, g, http.StatusNotFound)
	})
}
