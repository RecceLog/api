// Package mapbox provides clients for the Mapbox APIs used server-side.
// It is intentionally free of any import from internal domain packages so it
// can be reused or replaced without touching business logic.
package mapbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Geocoder resolves geographic coordinates to a human-readable place name.
// Implementations must be safe for concurrent use.
type Geocoder interface {
	// ReverseGeocode returns the most relevant place name for the given
	// coordinates (city level). Returns an empty string without error when no
	// match is found; callers should treat that as "name unavailable".
	ReverseGeocode(ctx context.Context, lat, lng float64) (string, error)
}

// HTTPGeocoder calls the Mapbox Geocoding v5 API to resolve coordinates.
type HTTPGeocoder struct {
	accessToken string
	client      *http.Client
}

// NewHTTPGeocoder creates a geocoder backed by the given Mapbox access token.
func NewHTTPGeocoder(accessToken string) *HTTPGeocoder {
	return &HTTPGeocoder{
		accessToken: accessToken,
		client:      &http.Client{Timeout: 5 * time.Second},
	}
}

const geocodeEndpoint = "https://api.mapbox.com/geocoding/v5/mapbox.places/%f,%f.json"

// ReverseGeocode queries the Mapbox Geocoding v5 endpoint and returns the
// `text` field of the first feature (typically a city name). An HTTP or
// decoding error is returned as-is; an empty result set returns ("", nil).
func (g *HTTPGeocoder) ReverseGeocode(ctx context.Context, lat, lng float64) (string, error) {
	rawURL := fmt.Sprintf(geocodeEndpoint, lng, lat)

	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("mapbox: parse url: %w", err)
	}
	q := u.Query()
	q.Set("types", "place")
	q.Set("limit", "1")
	q.Set("access_token", g.accessToken)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("mapbox: build request: %w", err)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("mapbox: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mapbox: unexpected status %d", resp.StatusCode)
	}

	var body struct {
		Features []struct {
			Text string `json:"text"`
		} `json:"features"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("mapbox: decode response: %w", err)
	}

	if len(body.Features) == 0 {
		return "", nil
	}
	return body.Features[0].Text, nil
}
