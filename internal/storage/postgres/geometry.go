package postgres

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/RecceLog/api/internal/domain"
)

// srid is the WGS84 spatial reference system, matching the columns declared
// in the migration (GEOMETRY(..., 4326)).
const srid = 4326

// pointWKT renders a domain.Point as PostGIS-compatible Well-Known Text.
// Used with ST_GeomFromText($1, 4326) on the SQL side.
func pointWKT(p domain.Point) string {
	return fmt.Sprintf("POINT(%s %s)", ftoa(p.Lng), ftoa(p.Lat))
}

// lineStringWKT renders a domain.LineString as WKT. Returns an empty string
// if the line has fewer than 2 points — callers should validate first.
func lineStringWKT(ls domain.LineString) string {
	if len(ls) < 2 {
		return ""
	}
	var b strings.Builder
	b.WriteString("LINESTRING(")
	for i, p := range ls {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(ftoa(p.Lng))
		b.WriteByte(' ')
		b.WriteString(ftoa(p.Lat))
	}
	b.WriteByte(')')
	return b.String()
}

// ftoa formats a float with enough precision to round-trip WGS84 coordinates
// (~7 decimal places ≈ 11 mm at the equator is plenty).
func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// lineStringFromGeoJSON parses a GeoJSON LineString — the textual form
// returned by ST_AsGeoJSON(path) — into a domain.LineString.
func lineStringFromGeoJSON(s string) (domain.LineString, error) {
	var g struct {
		Type        string      `json:"type"`
		Coordinates [][]float64 `json:"coordinates"`
	}
	if err := json.Unmarshal([]byte(s), &g); err != nil {
		return nil, fmt.Errorf("parse geojson: %w", err)
	}
	if g.Type != "LineString" {
		return nil, fmt.Errorf("expected LineString, got %q", g.Type)
	}
	ls := make(domain.LineString, len(g.Coordinates))
	for i, c := range g.Coordinates {
		if len(c) < 2 {
			return nil, fmt.Errorf("geojson coord %d: expected [lng, lat], got %v", i, c)
		}
		ls[i] = domain.Point{Lng: c[0], Lat: c[1]}
	}
	return ls, nil
}
