package domain

// Point is a geographic coordinate WGS84 (SRID 4326).
type Point struct {
	Lng float64
	Lat float64
}

// Valid verifies that the coordinate is in the admitted geographical range.
func (p Point) Valid() bool {
	return p.Lng >= -180 && p.Lng <= 180 &&
		p.Lat >= -90 && p.Lat <= 90
}

// LineString is an ordered sequence of points: the path of the route.
type LineString []Point

// Valid verifies that a line string contains at least two valid points
func (ls LineString) Valid() bool {
	if len(ls) < 2 {
		return false
	}
	for _, p := range ls {
		if !p.Valid() {
			return false
		}
	}
	return true
}
