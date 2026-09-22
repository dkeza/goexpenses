# goexpenses
Simple expenses web application written in Go, using Echo and PostgreSQL.
You can define expenses and incomes, and then enter posts.
It is possible to enter amounts in RSD or EUR currency.

Project is using modules for dependency management.

The server-rendered UI uses locally bundled Bootstrap 5.3.8 assets. Custom
colors, spacing, responsive layouts, and light/dark themes are defined in
`static/css/main.css`; no frontend build step is required.

Requires Go 1.27.1 or newer.

Build and installation

Get source with

```
go get github.com/dkeza/goexpenses
```

Build binary

```
go build
```

Copy the executable to the server. HTML templates, static files and database
structure scripts are embedded in it and do not need to be copied separately.

Runtime configuration remains external. You can copy the example configuration
file

```
goexpenses.dev.ini
```

Rename goexpenses.dev.ini to goexpenses.ini, and enter settings.

Alternatively, use environment variables without an INI file. Environment
variables override values loaded from `goexpenses.ini`. Look in
`startgoexpenses.dev.bat` for a Windows example.

Configuration is loaded in this order:

1. Application defaults (`PORT=8080` and secure cookies enabled).
2. The optional `goexpenses.ini` file.
3. Environment variables, which override both defaults and INI values.

The application validates its configuration before connecting to external
services. `HOST`, `DATABASE_URL`, `MAIL_HOST`, `MAIL_PORT`, `MAIL_FROM`, and
`EXCHANGE_ID` are required. `HOST` must be an absolute HTTP(S) URL and
`DATABASE_URL` must be a PostgreSQL URL. `MAIL_PASSWORD` is optional for SMTP
servers which do not require authentication.

PostgreSQL is currently the only enabled database driver. Invalid configuration
is reported at startup without printing passwords, API keys, or database URLs.

The application creates the database schema automatically on first startup.
Later schema migrations are embedded in the executable, serialized with a
PostgreSQL advisory lock, and applied in transactions. The recorded schema
version is updated only after a migration succeeds. As with every database
deployment, create a backup before installing a new application version.

The schema enforces case-insensitive uniqueness for user names and e-mail
addresses, unique public record IDs, account membership uniqueness, and the
required account relationships for financial records. Migration 15 deliberately
fails if existing users or public IDs conflict, because choosing which identity
to keep cannot be done safely without an operator. Resolve the reported
duplicates and restart the application to retry the transactional migration.

The normal test suite skips the PostgreSQL integration test unless
`TEST_DATABASE_URL` is set. CI supplies a disposable PostgreSQL service and
tests fresh schema creation, the version 14 to current upgrade, registration, login,
posting, and database constraint enforcement.

Version bump

Create a dedicated branch with a clean working tree, then run:

```
./scripts/bump-version.sh
```

The script increments the application/schema version, registers and creates a
no-op migration for the new version, and runs all tests. Review and commit the
generated changes before opening a pull request. For a release that changes the
database schema, replace the generated `SELECT 1;` with the required SQL before
committing.

Exchange rates are refreshed in the background, so an unavailable rates service
does not block application startup or user requests. Failed or invalid responses
leave the last successfully stored rate unchanged.

Session cookies are secure by default. Set `COOKIE_SECURE=false` (or
`cookiesecure=false` in `goexpenses.ini`) only for local development over HTTP.

Browser responses include a nonce-based Content Security Policy compatible with
Google Analytics and AdSense, along with MIME-sniffing, framing, referrer, and
browser-permission protections. HSTS is enabled whenever secure cookies are
enabled, and is therefore omitted only in explicitly insecure local development.

Public authentication endpoints are protected by in-memory sliding-window rate
limits per client IP and normalized account identifier. Login allows 30 attempts
per IP and 5 per user name in 15 minutes; password reset allows 10 per IP and 3
per e-mail address per hour; registration allows 5 per IP and 3 per submitted
user name or e-mail address per hour. A successful login clears the user-name
limit. Registration conflicts use the same response as successful registration
so they do not expose existing user names or e-mail addresses. Limits reset when
the application process restarts.

Start binary exe

Database would be automatically created. EUR and RSD currency exchange rates would be automatically updated on start, and then once a day.
User must register with valid E-Mail. When reseting password, activation link is sent to E-Mail.

Working example:
https://goexpenses.kezic.net/

Credits to

* [Echo Web Framework](https://github.com/labstack/echo)
* [Bootstrap](https://getbootstrap.com/)
* [open exchange rates](https://openexchangerates.org)
