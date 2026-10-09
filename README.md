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
`TEST_DATABASE_URL` is set or a local, Git-ignored `.test-db-url` file exists.
The environment variable takes precedence. A local file can contain a URL
without a password, for example
`postgresql://goexpenses_test@127.0.0.1:5432/goexpenses_test?sslmode=disable`;
the PostgreSQL password can be kept in `~/.pgpass` (mode `0600`). The test user
must be able to create schemas in the test database. The test creates a uniquely
named temporary schema and drops it afterward; it does not alter existing
schemas. CI supplies a disposable PostgreSQL service and tests fresh schema
creation, the version 14 to current upgrade, registration, login, posting, and
database constraint enforcement.

Versions

The application version is assigned automatically by CI: every build is
stamped with the GitHub Actions run number and the short commit hash, for
example `v58 · 8e503d1`. It is shown in the navigation bar, logged at startup,
and appended to static file URLs so every deployment refreshes cached CSS and
JavaScript. Local builds report `dev`. No manual version bump is needed.

The database schema version is separate and changes only with real schema
changes. To add one, create `migrations/sql/NNN_description.sql` with the next
number, register it in `migrationPaths`, and set `migrations.SchemaVersion` to
that number; a test checks that they match. An application refuses to start
against a newer schema, so a binary rollback works for every release without
a schema change. Keep schema changes backward compatible and back up the
database before deploying them.

Exchange rates are refreshed in the background, so an unavailable rates service
does not block application startup or user requests. Failed or invalid responses
leave the last successfully stored rate unchanged.

Exchange-rate refreshes run daily at 07:00 and expired sessions are removed at
05:00 in the `Europe/Belgrade` time zone. Background jobs start once immediately,
observe application cancellation, and finish before the database connection is
closed during graceful shutdown.

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

### Administration

The admin panel is available at `/admin` after normal sign-in. Its user list can
be searched and filtered; user details allow blocking, unblocking, and permanently
deleting non-admin users. Deletion requires typing the exact user name and
removes sessions, password reset requests, and accounts used only by that user
with their financial records. Shared accounts and their records are retained.
User details show row counts for linked accounts, posts, expenses, and incomes,
including how many would be removed with the user.
Blocking immediately deletes the user's active sessions. The panel also shows
new audit events for exchange-rate refreshes, expired-session cleanup, SMTP
handoff attempts, sign-ins, explicit sign-outs, and block/unblock actions.
An `smtp_accepted` event means the configured SMTP server accepted the message;
it does not prove delivery to the recipient's mailbox. A pending event means
the process stopped before the result could be recorded. Events start being
recorded when this version is deployed; older history cannot be reconstructed.

Migration 21 grants admin access to an **existing, verified** account whose
normalized username is `keza`. On a fresh installation, register and verify
that account first, then promote it using a restricted database session:

```sql
UPDATE users SET is_admin = true
WHERE lower(btrim(username)) = 'keza' AND email_verified = true;
```

Registration never grants admin access. The panel refuses to block or delete admin
accounts, and every admin request checks the role stored in the database.

New registrations require E-Mail confirmation before sign-in. Confirmation links
expire after 24 hours and can be requested again from the sign-in page, at most
once every 15 minutes per unconfirmed account. Existing accounts remain verified
when upgrading to schema version 20. Configure working SMTP delivery and a public
`HOST` so new users can receive and open their confirmation links.

New posts can be filled from the NBS IPS QR code on a bill: the scanner reads
the amount and builds the description from the payee and payment purpose. It
uses the camera (Chrome on Android uses its built-in barcode detector, other
browsers the bundled jsQR library), or an image chosen from a file or pasted
with Ctrl+V. The image is decoded in the browser and never sent to the server.

The app can be installed on a phone or desktop as a Progressive Web App (in
Chrome: menu → *Add to Home screen* / *Install app*); it then opens in its own
window without the address bar. Installation requires HTTPS. The service worker
(`/sw.js`) caches only static files, which are refreshed on every deploy; pages
with financial data are always loaded from the server, and without a connection
a short offline page is shown.

Working example:
https://goexpenses.kezic.net/

Credits to

* [Echo Web Framework](https://github.com/labstack/echo)
* [Bootstrap](https://getbootstrap.com/)
* [jsQR](https://github.com/cozmo/jsQR) (Apache-2.0)
* [open exchange rates](https://openexchangerates.org)
