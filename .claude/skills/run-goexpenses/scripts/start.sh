#!/usr/bin/env bash
# Starts goexpenses locally on a fresh, disposable PostgreSQL database and seeds
# a verified user (tester / Lozinka12345!), an EUR rate and one expense type.
# Usage: start.sh [port]   (default 8091). Stop with stop.sh.
set -euo pipefail

PORT="${1:-8091}"
DB_NAME="goexpenses_runcheck"
REPO="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
WORK="${RUN_WORK_DIR:-${TMPDIR:-/tmp}/goexpenses-run}"
mkdir -p "$WORK"

: "${TEST_DATABASE_URL:?TEST_DATABASE_URL must point to a local PostgreSQL database}"
ADMIN_URL="$TEST_DATABASE_URL"
DB_URL="$(echo "$ADMIN_URL" | sed -E "s#/[^/?]+(\?|$)#/${DB_NAME}\1#")"

lsof -ti:"$PORT" -sTCP:LISTEN | xargs -r kill || true
psql "$ADMIN_URL" -qc "DROP DATABASE IF EXISTS ${DB_NAME}" -c "CREATE DATABASE ${DB_NAME}"

(cd "$REPO" && go build -o "$WORK/goexpenses" .)

# Mail and exchange-rate settings are required by config validation only; the
# rate refresh fails with HTTP 401 in the log, which is expected.
(cd "$REPO" && DATABASE_TYPE=postgres DATABASE_URL="$DB_URL" PORT="$PORT" \
  HOST="http://localhost:$PORT" COOKIE_SECURE=false EXCHANGE_ID=dummy \
  MAIL_HOST=localhost MAIL_PORT=2525 MAIL_FROM=test@example.com \
  nohup "$WORK/goexpenses" >"$WORK/server.log" 2>&1 &)
timeout 60 bash -c "until curl -sf -o /dev/null http://localhost:$PORT/login; do sleep 1; done" \
  || { tail -20 "$WORK/server.log"; exit 1; }

# Playwright pinned to the Chromium build already in ~/.cache/ms-playwright.
if [ ! -d "$WORK/pw/node_modules/playwright" ]; then
  mkdir -p "$WORK/pw"
  (cd "$WORK/pw" && npm init -y >/dev/null && npm i -s playwright@1.49.1)
fi
cp "$(dirname "$0")"/*.js "$WORK/pw/"

(cd "$WORK/pw" && node register.js "http://localhost:$PORT")
psql "$DB_URL" -q \
  -c "UPDATE users SET email_verified = true WHERE username = 'tester'" \
  -c "INSERT INTO currencies (code, rate, date) VALUES ('EUR', 117.4657, now())"

echo "Running: http://localhost:$PORT  (tester / Lozinka12345!)"
echo "Log: $WORK/server.log   Drivers: $WORK/pw"
