-- +goose Up
-- +goose StatementBegin

CREATE EXTENSION IF NOT EXISTS postgis;

-- enums
CREATE TYPE vehicle_type AS ENUM ('FOOT', 'BIKE', 'MOTORCYCLE', 'CAR');
CREATE TYPE note_type AS ENUM ('INDICATION', 'WARNING');
CREATE TYPE direction_type AS ENUM ('LEFT', 'RIGHT', 'STRAIGHT', 'CHICANE');

-- users
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    keycloak_sub TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    description TEXT,
    avatar_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- routes
CREATE TABLE routes (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    author_id UUID REFERENCES users(id) ON DELETE SET NULL,
    name TEXT,
    description TEXT,
    cover_photo_url TEXT,
    path GEOMETRY(LineString, 4326) NOT NULL,
    -- length_m is a stored generated column: PostGIS recomputes it automatically
    -- whenever path is written so it is always in sync with the geometry.
    -- ST_Length(geometry::geography) uses the WGS84 spheroid (Vincenty) and
    -- returns metres.
    length_m FLOAT8 GENERATED ALWAYS AS (ST_Length(path::geography)) STORED,
    start_city TEXT,
    finish_city TEXT,
    vehicles vehicle_type[] NOT NULL CHECK (array_length(vehicles, 1) > 0),
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
    name TEXT,
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
    severity INT CHECK (severity BETWEEN 1 AND 7),
    direction direction_type,
    description VARCHAR(255),
    CHECK (
        (type = 'INDICATION' AND direction IS NOT NULL) OR
        (type = 'WARNING')
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

CREATE TRIGGER routes_set_updated_at
    BEFORE UPDATE ON routes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER notes_update_note_set_updated_at
    AFTER INSERT OR UPDATE OR DELETE ON notes
    FOR EACH ROW EXECUTE FUNCTION update_note_set_updated_at();
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
DROP TYPE IF EXISTS direction_type;
DROP TYPE IF EXISTS note_type;
DROP TYPE IF EXISTS vehicle_type;
-- +goose StatementEnd