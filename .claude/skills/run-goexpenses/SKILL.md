---
name: run-goexpenses
description: Launch goexpenses locally on a disposable PostgreSQL database with a seeded, verified user and drive it in headless Chromium (Playwright) to check a change in the real app.
---

# Run goexpenses and drive it in a browser

Verified on WSL2 (Linux) with Go, local PostgreSQL 16 on 127.0.0.1:5432,
Node 22 and the Playwright Chromium build 1148 cached in `~/.cache/ms-playwright`.

## Prerequisites

- `TEST_DATABASE_URL` is set in the environment and points to the local
  PostgreSQL as a role that can `CREATE DATABASE`. Never print it (it holds the
  password). The scripts only use it to create/drop `goexpenses_runcheck`; the
  test database itself is not touched.
- No `goexpenses.ini` in the repo root (config comes from env vars).

## Start

```bash
RUN_WORK_DIR=<scratchpad>/run bash .claude/skills/run-goexpenses/scripts/start.sh 8091
```

It recreates `goexpenses_runcheck`, builds the binary, starts the server in the
background, waits for `/login`, installs `playwright@1.49.1` once into
`$RUN_WORK_DIR/pw`, registers `tester` / `Lozinka12345!` through the UI, marks
the e-mail verified in the DB and inserts an EUR rate (117.4657).

Config validation requires HOST, MAIL_HOST, MAIL_PORT, MAIL_FROM and
EXCHANGE_ID, so the script passes dummy values. It also sets a random `TOTPKEY`
(unless one is already in the environment), so two-factor authentication can
be turned on at `/account/2fa`; compute codes from the `#totp-secret` text. Expected noise in
`server.log`: `refresh exchange rates failed ... HTTP 401`.

## Drive

```bash
cd <scratchpad>/run/pw && node smoke.js http://localhost:8091 <scratchpad>/run
```

`smoke.js` logs in, creates an expense type ("EPS (Struja)"), saves a post with
a changed date on `/posts` and screenshots the page. Copy it for other flows.
Look at the screenshot. Useful selectors: `#description`, `#expense_id`,
`#date`, `#amount`, `#amounte`, forms `form[action="/posts/save"]` and
`form[action="/expenses/save"]`.

The UI is in English for a new user (language switch in the navbar).

## Stop

```bash
bash .claude/skills/run-goexpenses/scripts/stop.sh 8091
```

## Gotchas

- AdSense (`googlesyndication.com`) and Google Analytics fail with
  `ERR_CONNECTION_REFUSED` offline; ignore them (`smoke.js` filters them out).
- A new user has no expense types; `/posts` cannot save an expense until one is
  created.
- Login only accepts users with `email_verified = true`; there is no mail
  server, so verify in the DB.
