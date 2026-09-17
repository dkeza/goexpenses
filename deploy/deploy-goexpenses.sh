#!/usr/bin/env bash

set -Eeuo pipefail

readonly staged_binary="${1:-/home/pi/goexpenses.new}"
readonly target_binary="/home/pi/server/goexpenses"
readonly service_name="goexpenses"
readonly health_url="http://127.0.0.1:8080/"
readonly timestamp="$(date -u +%Y%m%d-%H%M%S)"
readonly backup_binary="${target_binary}.backup.${timestamp}"
readonly next_binary="${target_binary}.next.$$"

cleanup() {
    rm -f -- "$next_binary"
}
trap cleanup EXIT

fail() {
    echo "Deploy failed: $*" >&2
    exit 1
}

restore_previous_binary() {
    echo "Restoring $backup_binary"
    cp -a -- "$backup_binary" "$next_binary"
    mv -f -- "$next_binary" "$target_binary"

    if ! sudo systemctl restart "$service_name"; then
        echo "Rollback failed to restart $service_name" >&2
        return 1
    fi

    if ! sudo systemctl is-active --quiet "$service_name"; then
        echo "Rollback completed, but $service_name is not active" >&2
        return 1
    fi

    echo "Previous binary restored and $service_name restarted"
}

[[ -f "$staged_binary" ]] || fail "staged binary not found: $staged_binary"
[[ -x "$staged_binary" ]] || fail "staged binary is not executable: $staged_binary"
[[ -f "$target_binary" ]] || fail "current binary not found: $target_binary"
[[ "$staged_binary" != "$target_binary" ]] || fail "staged and target paths must be different"

binary_description="$(file -b -- "$staged_binary")"
case "$binary_description" in
    *ELF*ARM*) ;;
    *) fail "staged file is not a Linux ARM executable: $binary_description" ;;
esac

echo "Staged binary: $binary_description"
echo "SHA-256: $(sha256sum "$staged_binary" | cut -d ' ' -f 1)"

cp -a -- "$target_binary" "$backup_binary"
echo "Backup created: $backup_binary"

install -m 0755 -- "$staged_binary" "$next_binary"
mv -f -- "$next_binary" "$target_binary"

if ! sudo systemctl restart "$service_name"; then
    echo "Service restart failed" >&2
    restore_previous_binary
    exit 1
fi

for ((attempt = 1; attempt <= 30; attempt++)); do
    if sudo systemctl is-active --quiet "$service_name" && \
        curl --fail --silent --show-error --output /dev/null "$health_url"; then
        echo "Deploy succeeded: $service_name is active and the HTTP check passed"
        echo "Rollback binary: $backup_binary"
        exit 0
    fi
    sleep 1
done

echo "Service did not pass its health check within 30 seconds" >&2
sudo journalctl -u "$service_name" -n 20 --no-pager >&2 || true
restore_previous_binary
exit 1
