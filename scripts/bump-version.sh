#!/usr/bin/env bash

set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

fail() {
    echo "Version bump failed: $*" >&2
    exit 1
}

for command in git go grep sed; do
    command -v "$command" >/dev/null 2>&1 || fail "required command is missing: $command"
done

branch="$(git branch --show-current)"
[[ -n "$branch" ]] || fail "detached HEAD is not supported"
case "$branch" in
    master | main) fail "create a version-bump branch before running this script" ;;
esac

[[ -z "$(git status --porcelain)" ]] || fail "working tree must be clean"

current_version="$(sed -nE 's/^const CurrentVersion = ([0-9]+)$/\1/p' migrations/migrations.go)"
[[ "$current_version" =~ ^[0-9]+$ ]] || fail "cannot read CurrentVersion from migrations/migrations.go"

next_version=$((current_version + 1))
((next_version <= 999)) || fail "version $next_version cannot use the three-digit migration filename format"

current_padded="$(printf '%03d' "$current_version")"
next_padded="$(printf '%03d' "$next_version")"
next_migration="migrations/sql/${next_padded}_current_version.sql"

grep -Eq "^[[:space:]]*${current_version}:[[:space:]]+\"sql/${current_padded}_[^\"]+\",$" migrations/migrations.go ||
    fail "migration $current_version is not registered"
grep -Eq "^[[:space:]]*${next_version}:" migrations/migrations.go &&
    fail "migration $next_version is already registered"
[[ ! -e "$next_migration" ]] || fail "migration file already exists: $next_migration"

sed -i -E \
    "s/^const CurrentVersion = ${current_version}$/const CurrentVersion = ${next_version}/" \
    migrations/migrations.go
sed -i -E \
    "/^[[:space:]]*${current_version}:[[:space:]]+\"sql\/${current_padded}_[^\"]+\",$/a\\	${next_version}: \"sql/${next_padded}_current_version.sql\"," \
    migrations/migrations.go
printf 'SELECT 1;\n' > "$next_migration"

gofmt -w migrations/migrations.go
go test ./...

echo
echo "Application version bumped from v${current_version} to v${next_version}."
echo "Created no-op migration: ${next_migration}"
echo
git status --short
