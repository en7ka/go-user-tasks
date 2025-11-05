# Go User Tasks Server

Simple HTTP API to manage users, tasks, referrals, and leaderboards. Built with Go, Postgres, JWT, and docker-compose.

## Endpoints (all require `Authorization: Bearer <JWT>`)

- `GET /users/{id}/status` — user info + completed tasks
- `GET /users/leaderboard?limit=10` — top users by points
- `POST /users/{id}/task/complete` — body: `{"task":"subscribe_twitter"}`
- `POST /users/{id}/referrer` — body: `{"referrer_id": 2}`

Admin access: include `"role":"admin"` claim in the JWT to access any user's data. Regular users can only access their own `{id}`.

## Quick start

```bash
docker compose up --build
```

Health check:
```bash
curl -i http://localhost:8080/health
```

## Create sample users

Use psql inside the db container:
```bash
docker compose exec -T db psql -U app -d app -c "INSERT INTO users (username) VALUES ('alice'),('bob'),('carol') RETURNING *;"
```

Copy the returned user IDs to mint JWTs with `tools/jwtgen` (or use any JWT tool).

## Generate JWTs for testing

Build the small helper:
```bash
go build -o ./jwtgen ./tools/jwtgen
./jwtgen -sub 1 -secret dev-secret        # user 1
./jwtgen -sub 999 -role admin -secret dev-secret  # admin
```

## Example requests

```bash
# Get own status (user id 1)
TOKEN=$(./jwtgen -sub 1 -secret dev-secret)
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/users/1/status

# Complete a task
curl -X POST -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"task":"subscribe_twitter"}' \
     http://localhost:8080/users/1/task/complete

# Set referrer
curl -X POST -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"referrer_id":2}' \
     http://localhost:8080/users/1/referrer

# Leaderboard
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/users/leaderboard?limit=5
```

## Migrations

Uses `golang-migrate` via a container in `docker-compose.yml`. SQL files are in `./migrations`.

## Notes

- Points from tasks are given once per task per user.
- Referral bonuses (defaults): referred +10, referrer +50.
- Config via env: `DB_DSN`, `JWT_SECRET`, `HTTP_PORT`.
```
