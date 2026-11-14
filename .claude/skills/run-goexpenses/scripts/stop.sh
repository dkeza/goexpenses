#!/usr/bin/env bash
# Stops the server started by start.sh and drops its disposable database.
set -uo pipefail
PORT="${1:-8091}"
lsof -ti:"$PORT" -sTCP:LISTEN | xargs -r kill || true
sleep 1
psql "${TEST_DATABASE_URL:?}" -qc "DROP DATABASE IF EXISTS goexpenses_runcheck" && echo "Stopped and dropped goexpenses_runcheck"
