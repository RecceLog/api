-- +goose Up
-- +goose StatementBegin

CREATE EXTENSION IF NOT EXISTS postgis;

-- enums
CREATE TYPE vehicle_type AS ENUM ('FOOT', 'BIKE', 'MOTORCYCLE', 'CAR');
CREATE TYPE note_type AS ENUM ('INDICATION', 'WARNING');
CREATE TYPE direction_type AS ENUM ('LEFT', 'RIGHT', 'STRAIGHT', 'CHICANE');

-- array_has_no_duplicates returns true when arr contains no repeated element.
-- A CHECK constraint cannot contain a subquery, so the DISTINCT comparison is
-- wrapped in this IMMUTABLE function and called from the routes.vehicles CHECK.
CREATE OR REPLACE FUNCTION array_has_no_duplicates(arr anyarray)
RETURNS boolean
LANGUAGE sql IMMUTABLE
AS $$
    SELECT cardinality(arr) = cardinality(ARRAY(SELECT DISTINCT unnest(arr)))
$$;

-- users
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    keycloak_sub TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    description TEXT,
    -- avatar_content_type is NULL when the user has no custom avatar (the
    -- /v1/users/{id}/profile_pic endpoint then serves the default image); when
    -- set it is the MIME type of the stored <id>.<ext> file.
    avatar_content_type TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- routes
CREATE TABLE routes (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    author_id UUID REFERENCES users(id) ON DELETE SET NULL,
    name VARCHAR(120),
    description VARCHAR(1000),
    -- path is a LineString; the bounds mirror the domain rules (at least two
    -- points to be a valid line, at most routePathMaxPoints) so an empty or
    -- oversized path is rejected at the DB too. ~53 pts/km → 5 000 points covers
    -- a dense ~90 km route.
    path GEOMETRY(LineString, 4326) NOT NULL CHECK (ST_NPoints(path) BETWEEN 2 AND 5000),
    -- length_m is a stored generated column: PostGIS recomputes it automatically
    -- whenever path is written so it is always in sync with the geometry.
    -- ST_Length(geometry::geography) uses the WGS84 spheroid (Vincenty) and
    -- returns metres.
    length_m FLOAT8 GENERATED ALWAYS AS (ST_Length(path::geography)) STORED,
    -- start/finish city are NOT NULL: the API fills them from the path via
    -- reverse geocoding when the client omits them (falling back to 'N/A').
    start_city VARCHAR(120) NOT NULL,
    finish_city VARCHAR(120) NOT NULL,
    -- at least one vehicle, at most the 4 enum values, and no duplicates.
    vehicles vehicle_type[] NOT NULL
        CHECK (array_length(vehicles, 1) BETWEEN 1 AND 4 AND array_has_no_duplicates(vehicles)),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- index for geographical queries ("nearby routes").
-- Built on the geography expression, not the bare geometry: the nearby
-- queries filter/order on `path::geography` (ST_DWithin / KNN <->), and a
-- GIST index on the geometry column would NOT accelerate the casted
-- expression (PostGIS GIST indexes are type-specific), forcing a seq scan.
CREATE INDEX idx_routes_path_gist ON routes USING GIST ((path::geography));
-- index for vehicle type
CREATE INDEX idx_routes_vehicles_gin ON routes USING GIN (vehicles);

-- waypoints
CREATE TABLE waypoints (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    route_id UUID NOT NULL REFERENCES routes(id) ON DELETE CASCADE,
    position GEOMETRY(Point, 4326) NOT NULL,
    "order" INT NOT NULL,
    name TEXT
);

CREATE INDEX idx_waypoints_route ON waypoints (route_id);
CREATE INDEX idx_waypoints_position_gist ON waypoints USING GIST (position);

-- note sets
CREATE TABLE note_sets (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    route_id UUID NOT NULL REFERENCES routes(id) ON DELETE CASCADE,
    author_id UUID REFERENCES users(id) ON DELETE SET NULL,
    name VARCHAR(120),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_note_sets_route ON note_sets (route_id);

-- notes
CREATE TABLE notes (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    set_id UUID NOT NULL REFERENCES note_sets(id) ON DELETE CASCADE,
    position GEOMETRY(Point, 4326) NOT NULL,
    "order" INT NOT NULL,
    type note_type NOT NULL,
    direction direction_type,
    severity INT CHECK (severity BETWEEN 1 AND 7),
    description VARCHAR(255),
    -- an indication must carry a direction; a warning need not.
    CHECK (
        (type = 'INDICATION' AND direction IS NOT NULL) OR
        (type = 'WARNING')
    ),
    -- severity is mandatory for an indication whose direction is not STRAIGHT
    -- (a straight indication, or any warning, may omit it).
    CHECK (
        type <> 'INDICATION' OR direction = 'STRAIGHT' OR severity IS NOT NULL
    )
);

CREATE INDEX idx_notes_set ON notes (set_id);
CREATE INDEX idx_notes_position_gist ON notes USING GIST (position);

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- trigger to update a note set when a note is updated
CREATE OR REPLACE FUNCTION update_note_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
UPDATE note_sets
SET updated_at = now()
WHERE id = COALESCE(NEW.set_id, OLD.set_id);
RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- A note must sit within 50 metres (geodesic) of its route's path: a note that
-- is nowhere near the route makes no sense. This relates a note to the route
-- geometry through note_sets, which a single-row CHECK cannot reach, so it is a
-- BEFORE trigger. It raises check_violation (23514), which the storage layer
-- maps to a 422 validation error.
CREATE OR REPLACE FUNCTION enforce_note_near_path()
RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM note_sets ns
        JOIN routes r ON r.id = ns.route_id
        WHERE ns.id = NEW.set_id
          AND ST_DWithin(r.path::geography, NEW.position::geography, 50)
    ) THEN
        RAISE EXCEPTION 'note position is more than 50 m from the route path'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER routes_set_updated_at
    BEFORE UPDATE ON routes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER notes_update_note_set_updated_at
    AFTER INSERT OR UPDATE OR DELETE ON notes
    FOR EACH ROW EXECUTE FUNCTION update_note_set_updated_at();

CREATE TRIGGER notes_enforce_near_path
    BEFORE INSERT OR UPDATE OF position, set_id ON notes
    FOR EACH ROW EXECUTE FUNCTION enforce_note_near_path();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS users_set_updated_at ON users;
DROP TRIGGER IF EXISTS routes_set_updated_at ON routes;
DROP FUNCTION IF EXISTS set_updated_at();
DROP TABLE IF EXISTS notes;
DROP TABLE IF EXISTS note_sets;
DROP TABLE IF EXISTS waypoints;
DROP TABLE IF EXISTS routes;
DROP TABLE IF EXISTS users;
DROP FUNCTION IF EXISTS enforce_note_near_path();
DROP FUNCTION IF EXISTS array_has_no_duplicates(anyarray);
DROP TYPE IF EXISTS direction_type;
DROP TYPE IF EXISTS note_type;
DROP TYPE IF EXISTS vehicle_type;
-- +goose StatementEnd