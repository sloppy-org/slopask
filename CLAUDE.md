# slopask

Anonymous Q&A tool for university lectures. Deployed at ask.sloppy.at.

## Build

```
make build
```

## Run

```
./slopask serve --bind 127.0.0.1 --port 8430 --data-dir ./data --uploads-dir ./uploads
./slopask create-room --title "WSD SS2026"
./slopask list-rooms
```

## Architecture

- `cmd/slopask/main.go` -- CLI entry point (serve, create-room, list-rooms)
- `internal/store/` -- SQLite data layer (modernc.org/sqlite, pure Go)
- `internal/server/` -- HTTP server (go-chi/chi/v5), SSE broker, file uploads
- `internal/server/static/` -- embedded static files (room.html, admin.html, style.css, app.js)

## Access model

- Student URL: `/r/{slug}` (12-char lowercase alphanumeric)
- Admin URL: `/admin/{admin_token}` (24-char mixed-case alphanumeric)
- External API: `/api/v0/rooms/{admin_token}/...` (slopcast integration)

## No tracking

No cookies, no IP logging, no analytics. Voter ID is a client-side localStorage UUID.
