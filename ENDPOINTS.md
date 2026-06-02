# RecceLog API — Endpoint reference

This document describes every HTTP operation exposed by the API and, for each
one, the exact flow a request goes through: the middleware it crosses, what the
handler validates, which use case it calls, and what it returns.

## Conventions shared by every request

**Global middleware chain.** Before reaching any handler, every request passes
through (in order): `RequestID` (assigns a request id), `CORS` (answers
preflight `OPTIONS` and enforces the allowed origins; the custom `Latitude` /
`Longitude` headers are explicitly allowed), `ClientIP` (resolves the client
address used for rate limiting), `Logger`, `Recoverer` (turns a panic into a
500 instead of crashing the process), the per-IP rate limiter (when
`API_RATE_LIMIT_PER_MIN` > 0; over-limit requests get `429`), and a 30-second
request `Timeout`.

**Authentication.** Endpoints marked *protected* additionally go through the
`Authenticate` middleware. It extracts the Bearer token from the `Authorization`
header and validates it against Keycloak (signature verified via JWKS, RS256
only, matching issuer, and matching audience when `KEYCLOAK_AUDIENCE` is set).
If the token is missing or invalid the request is rejected with `401`.
Otherwise the corresponding local user is provisioned just-in-time (upserted by
its Keycloak subject, so a first-time user is created in the application
database) and placed in the request context, after which processing continues.

**Request bodies.** Handlers that accept a JSON body decode it through a shared
helper that caps the body at 4 MiB (`http.MaxBytesReader`) and rejects unknown
fields. A malformed, oversized, or unknown-field payload is answered with `400`.

**Error mapping.** Domain errors are translated to HTTP status codes in one
place and serialized as RFC 7807 `application/problem+json`: a validation error
becomes `422`, a missing resource becomes `404`, a forbidden action becomes
`403`, and anything unexpected becomes `500`.

**Layering.** Handlers are thin transport adapters: they parse the URL/headers,
decode the body, read the caller from the context, call **one** use case in the
application layer (`internal/app`) and map the result. All orchestration,
transactions and authorization live in the application layer; the per-aggregate
services and storage sit below it.

---

# List all routes

**Endpoint:** `GET /v1/routes`
**Handler(s):** `handleListRoutes`

This endpoint is public. The handler calls the route use case `List`, which
delegates to the route service and performs a single-table scan returning a
lightweight summary of every route (id, name, cities, length, vehicles — no
path geometry and no notes, to keep the payload small). The summaries are mapped
to the response DTO and returned with `200`. Any failure surfaces as `500`.

---

# Get a route with its note sets

**Endpoint:** `GET /v1/routes/{id}`
**Handler(s):** `handleGetRoute`

This endpoint is public. The handler first parses `{id}` as a UUID and rejects a
malformed value with `400`. It then calls the route use case `GetDetail`, which
composes two reads: the route with its waypoints (returning `404` if the route
does not exist) and all of its note sets with their notes pre-loaded (two
queries, no N+1 regardless of how many sets there are). The combined aggregate
is mapped to the detail DTO and returned with `200`.

---

# List routes within a radius

**Endpoint:** `GET /v1/routes/range/{radius}`
**Handler(s):** `handleListRoutesInRange`

This endpoint is public and powers the map's "nearby" search. The handler parses
`{radius}` (meters) from the path and the `Latitude` / `Longitude` headers,
answering `400` if any of them is missing or not a number. It also reads the
optional, repeatable `vehicle` query parameter (e.g. `?vehicle=CAR&vehicle=BIKE`)
and maps the raw values to domain vehicles. It then calls the route use case
`ListInRange`, which validates the inputs in the service — coordinates must be
in range and the radius must be positive and at most 500 km, otherwise `422` —
and runs a PostGIS spatial query (`ST_DWithin` over a GIST index), optionally
filtering by vehicle. The matching summaries are returned with `200`.

---

# Create a route with an initial note set

**Endpoint:** `POST /v1/routes`
**Handler(s):** `Authenticate`, `handleCreateRoute`

This endpoint is protected by the `Authenticate` middleware, which grabs the
Keycloak token and validates it; if it is not valid the request is rejected with
`401`, otherwise the user is provisioned in the application database if it does
not exist yet and the processing continues. After auth, the handler decodes and
size-limits the JSON body (`400` on a malformed/oversized/unknown-field body),
maps it to a domain route and an optional domain note set, reads the
authenticated caller from the context, and calls the route use case `Create`.

That use case owns the whole creation flow. It requires the request to include a
note set — a route is always created together with its first set of notes — and
answers `422` if it is missing. It attributes both the route and the note set to
the caller (server-side, so the client cannot spoof another author). It then
fills any start/finish city the client omitted by reverse-geocoding the first
and last path point via Mapbox **before** opening the transaction (so the
external call never holds a database connection); when geocoding fails or
resolves to nothing the city falls back to the `"N/A"` placeholder, so the two
NOT NULL city columns are always satisfied and a Mapbox outage never blocks
creation. Finally it opens a single transaction in which it inserts the route
(validating its invariants — at least two and at most 5 000 path points, 1–4
distinct vehicles, field-length caps — → `422`, batching its waypoints) and the
note set (validating it, including the rule that a non-straight indication note
must carry a severity, → `422`, batching its notes). Each note must also lie
within 50 m of the route path — a note nowhere near the route is rejected with
`422` (enforced by a PostGIS trigger). If either insert fails the whole
transaction rolls back so nothing is persisted. On success the handler returns
`201` with the full route-detail representation.

---

# Delete a route

**Endpoint:** `DELETE /v1/routes/{id}`
**Handler(s):** `Authenticate`, `handleDeleteRoute`

This endpoint is protected. After authentication, the handler parses `{id}`
(`400` if malformed), reads the caller from the context, and calls the route use
case `Delete`. The use case looks up the route's author — returning `404` if the
route does not exist — and authorizes the action: only the author may delete the
route, otherwise it returns `403`. When authorized, it deletes the route; the
schema cascades the delete to the route's waypoints, note sets and notes. The
handler returns `204` with no body.

---

# Append a note set to a route

**Endpoint:** `POST /v1/routes/{id}/notes`
**Handler(s):** `Authenticate`, `handleAddNoteSet`

This endpoint is protected. After authentication, the handler parses the route
`{id}` (`400` if malformed), decodes and size-limits the body (`400` on a bad
body), reads the caller, and calls the route use case `AddNoteSet`. This is the
deliberate exception to the ownership rule: **any** authenticated user may add a
note set to **any** route, so there is no author check here. The use case stamps
the note set with the route id and attributes it to the caller, then inserts the
note set and its notes in a single transaction (validation failures, including a
note further than 50 m from the route path, → `422`). If the target route does
not exist the foreign-key violation is translated to `404`. On success the
handler returns `201` with the created note set.

---

# Add a waypoint to a route

**Endpoint:** `POST /v1/routes/{id}/waypoints`
**Handler(s):** `Authenticate`, `handleAddWaypoint`

This endpoint is protected. After authentication, the handler parses the route
`{id}` (`400` if malformed), decodes and size-limits the body (`400`), reads the
caller, and calls the route use case `AddWaypoint`. The use case authorizes the
action first — it looks up the route's author and allows the call only if the
caller is that author (`404` if the route is missing, `403` otherwise) — then
validates the waypoint (`422` on invalid coordinates) and inserts it. The
handler returns `201` with the persisted waypoint.

---

# Update a waypoint of a route

**Endpoint:** `PATCH /v1/routes/{id}/waypoints/{waypointID}`
**Handler(s):** `Authenticate`, `handleUpdateWaypoint`

This endpoint is protected. After authentication, the handler parses both the
route `{id}` and the `{waypointID}` (`400` if either is malformed), decodes and
size-limits the body (`400`), stamps the body with the waypoint id, reads the
caller, and calls the route use case `UpdateWaypoint`. The use case authorizes
the caller against the route's author (`404` if the route is missing, `403` if
not the author), validates the new waypoint values (`422`), and updates the row.
The update is guarded so a waypoint belonging to a different route cannot be
edited through the wrong URL — a mismatch surfaces as `404`. The handler returns
`200` with the updated waypoint.

---

# Delete a waypoint of a route

**Endpoint:** `DELETE /v1/routes/{id}/waypoints/{waypointID}`
**Handler(s):** `Authenticate`, `handleDeleteWaypoint`

This endpoint is protected. After authentication, the handler parses the route
`{id}` and the `{waypointID}` (`400` if malformed), reads the caller, and calls
the route use case `DeleteWaypoint`. The use case authorizes the caller against
the route's author (`404` if the route is missing, `403` if not the author) and
deletes the waypoint, guarded by the route id so a waypoint of another route
cannot be removed through the wrong URL (mismatch → `404`). The handler returns
`204` with no body.

---

# Get a user's public profile

**Endpoint:** `GET /v1/users/{id}`
**Handler(s):** `handleGetUser`

This endpoint is public — it backs the author profile shown next to routes and
note sets. The handler parses `{id}` as a UUID (`400` if malformed) and calls the
user use case `GetByID`, which returns the public profile (display name,
description) or `404` if no such user exists. The `avatar_url` field in the
response is derived from the id (it always points at the profile-picture
endpoint below). On success it returns `200` with the profile DTO.

---

# Get a user's profile picture

**Endpoint:** `GET /v1/users/{id}/profile_pic`
**Handler(s):** `handleGetProfilePic`

This endpoint is public and returns the actual avatar image bytes. The handler
parses `{id}` as a UUID (`400` if malformed) — and crucially builds the file
name from the **parsed** UUID, never from the raw URL, so the path cannot escape
the avatars directory. It calls the user use case `GetByID` (`404` if the user
does not exist) to read the stored avatar content type: if none is set it serves
the default image, otherwise it serves the user's `<id>.<ext>` file with that
content type (falling back to the default if the file is unexpectedly missing).
The image is served from the avatars directory with a `Cache-Control` header for
client/proxy caching.

---

# Get the current user

**Endpoint:** `GET /v1/users/me`
**Handler(s):** `Authenticate`, `handleGetMe`

This endpoint is protected. Because the `Authenticate` middleware has already
validated the token and placed the (just-in-time provisioned) user in the
request context, the handler simply reads that user and returns its profile with
`200`. If the context unexpectedly carries no user it answers `401`. No extra
database call is needed — the user was already resolved during authentication.

---

# Get the notes of a note set

**Endpoint:** `GET /v1/notes/{setID}`
**Handler(s):** `handleGetNotes`

This endpoint is public. The handler parses `{setID}` as a UUID (`400` if
malformed) and calls the note use case `GetNotes`, which first probes that the
set exists — distinguishing a missing set (`404`) from an existing but empty one
— and then returns the set's notes ordered by their `order` field. On success it
returns `200` with the array of notes.

---

# Delete a note set

**Endpoint:** `DELETE /v1/notes/{setID}`
**Handler(s):** `Authenticate`, `handleDeleteNoteSet`

This endpoint is protected. After authentication, the handler parses `{setID}`
(`400` if malformed), reads the caller, and calls the note use case
`DeleteNoteSet`. The use case looks up the note set's author — `404` if the set
does not exist — and authorizes the action: only that author may delete the set,
otherwise `403`. When authorized it deletes the set; the schema cascades to its
notes. The handler returns `204` with no body.

---

# Update a single note

**Endpoint:** `PATCH /v1/notes/{setID}/{noteID}`
**Handler(s):** `Authenticate`, `handleUpdateNote`

This endpoint is protected. After authentication, the handler parses both
`{setID}` and `{noteID}` (`400` if either is malformed), decodes and size-limits
the body (`400`), stamps the body with the note id, reads the caller, and calls
the note use case `UpdateNote`. The use case authorizes the caller against the
note set's author (`404` if the set is missing, `403` if not the author),
validates the new note values — including that the new position is within 50 m
of the route path — (`422`), and updates the row, guarded by the set id so a
note belonging to a different set cannot be edited through the wrong URL
(mismatch → `404`). The handler returns `200` with the updated note.

---

# Delete a single note

**Endpoint:** `DELETE /v1/notes/{setID}/{noteID}`
**Handler(s):** `Authenticate`, `handleDeleteNote`

This endpoint is protected. After authentication, the handler parses `{setID}`
and `{noteID}` (`400` if malformed), reads the caller, and calls the note use
case `DeleteNote`. The use case authorizes the caller against the note set's
author (`404` if the set is missing, `403` if not the author) and deletes the
note, guarded by the set id so a note of another set cannot be removed through
the wrong URL (mismatch → `404`). The handler returns `204` with no body.

---

# Operational endpoints

## Health check

**Endpoint:** `GET /health`
**Handler(s):** `handleHealth`

Public and unauthenticated. The handler pings the database with a 2-second
budget and returns `200` with `{"status":"ok"}` when the database responds, or
`503` with `{"status":"degraded"}` otherwise. Used by orchestrators and load
balancers.

## API documentation (Swagger UI)

**Endpoint:** `GET /swagger/*`
**Handler(s):** `httpSwagger.WrapHandler`

Public. Serves the interactive Swagger UI generated from the handler annotations.
