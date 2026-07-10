# Keycloak (RecceLog auth)

Keycloak is the identity provider for RecceLog. It owns **everything
security-related** — credentials, tokens, sessions, password reset, email
verification. The application database stores only app-facing profile data
(display name, description, avatar, ...) and links to the identity via the
`keycloak_sub` column.

Keycloak runs as part of the API's `docker-compose.yml` (services `keycloak` +
`keycloak-db`), so a single `docker compose up` brings up the whole stack.

## What's here

- `realm-export.json` — the `reccelog` realm, imported on first boot:
  - self-registration enabled,
  - a public SPA client `reccelog-web` (Authorization Code + PKCE, redirect to
    `${KC_WEB_ORIGIN}` — see below),
  - a public mobile client `reccelog-mobile` (custom-scheme redirect),
  - an audience mapper adding `reccelog-api` to access tokens,
  - a seed `tester` user (`tester` / `${KC_TESTER_PASSWORD}`, default
    `test123`) for testing.

(The compose services live in `../docker-compose.yml`.)

## Per-environment configuration (`${...}` placeholders)

The realm file uses Keycloak's native env-var substitution
(<https://www.keycloak.org/server/importExport>): during `--import-realm`,
`${KC_WEB_ORIGIN}` and `${KC_TESTER_PASSWORD}` are resolved from the keycloak
container's environment. The syntax is `${VAR}` — **not** `${env.VAR}`.
`../docker-compose.yml` always defines both, with localhost-friendly defaults:

| Variable             | Default                 | Purpose                                                  |
| -------------------- | ----------------------- | -------------------------------------------------------- |
| `KC_WEB_ORIGIN`      | `http://localhost:4200` | sole redirect-URI origin / web origin of `reccelog-web`  |
| `KC_TESTER_PASSWORD` | `test123`               | password of the seed `tester` user                       |

Each deployed environment sets `KC_WEB_ORIGIN` to its own domain in its
SSM-provided `.env` (e.g. `https://reccelog.duckdns.org`), so **prod's
Keycloak only trusts prod origins**, test only trusts test's, and so on.

> **Gotcha — import only happens once.** `--import-realm` silently *skips* a
> realm that already exists in the keycloak-db volume, so edits to
> `realm-export.json` (or to the placeholder values) do NOT apply to a running
> stack. To re-import in local dev, drop the volumes and start fresh:
>
> ```sh
> docker compose down -v && docker compose up -d
> ```
>
> Deployed instances always import cleanly because every `terraform apply`
> starts from an empty volume.

## How user provisioning works

There is **no** Keycloak --> API webhook. The API provisions local users
**just-in-time**: the first time a request arrives with a valid Keycloak token,
the API's auth middleware validates it against this realm's JWKS and, if no
local row exists for that `sub`, creates one from the token claims. So a user
exists in the app DB after their first authenticated call — not at registration
time.

## Running

Everything is one compose project now:

```sh
cd ..            # the api/ directory
docker compose up --build -d
```

- Admin console: <http://localhost:8081> (admin / admin by default — override
  with `KC_ADMIN` / `KC_ADMIN_PASSWORD`).
- Realm issuer (token `iss`): `http://localhost:8081/realms/reccelog`.
- The API reaches Keycloak in-network at `http://keycloak:8080/realms/reccelog`
  (its `KEYCLOAK_DISCOVERY_URL`), while validating tokens against the public
  issuer above (its `KEYCLOAK_ISSUER_URL`).

## Getting a token for testing

The SPA client has Direct Access Grants enabled for convenience in dev:

```sh
curl -s -X POST \
  http://localhost:8081/realms/reccelog/protocol/openid-connect/token \
  -d grant_type=password \
  -d client_id=reccelog-web \
  -d username=tester \
  -d password=test123 | jq -r .access_token
```

Use the returned token as `Authorization: Bearer <token>` against the API's
write endpoints (e.g. `POST /v1/routes`).

## Configuration knobs (env)

| Variable             | Default                    | Purpose                              |
| -------------------- | -------------------------- | ------------------------------------ |
| `KC_ADMIN`           | `admin`                    | bootstrap admin username             |
| `KC_ADMIN_PASSWORD`  | `admin`                    | bootstrap admin password             |
| `KC_HOSTNAME`        | `http://localhost:8081`    | public base URL (drives token `iss`) |
| `KC_WEB_ORIGIN`      | `http://localhost:4200`    | origin trusted by `reccelog-web`     |
| `KC_TESTER_PASSWORD` | `test123`                  | seed `tester` user password          |
| `KC_PORT`            | `8081`                     | host port mapped to Keycloak's 8080  |
| `KC_DB_USER`         | `keycloak`                 | Keycloak DB user                     |
| `KC_DB_PASSWORD`     | `keycloak`                 | Keycloak DB password                 |
| `KC_DB_NAME`         | `keycloak`                 | Keycloak DB name                     |

> **Production note:** `start-dev` and the bootstrap admin are for local use
> only. For production switch to `start`, set a fixed hostname over HTTPS,
> persist the DB, and create a real admin.
