#!/usr/bin/env bash
# The panel sends X-Sha256. mawk does not honor awk IGNORECASE.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
script="$ROOT/internal/installscript/install-agent.sh"
fail() { echo "FAIL: $*" >&2; exit 1; }

sum="e4c0a514206ade91a36f6eb959cd77d50ec78a6c2047ed449bb42d60390c2708"
dir="$(mktemp -d)"
trap 'rm -rf "$dir"' EXIT

read_header() {
  LIKING_HEADER_TEST=1 bash --noprofile --norc "$script" x-sha256 "$1"
}

printf 'HTTP/1.1 200 OK\r\nContent-Type: application/octet-stream\r\nX-Sha256: %s\r\n\r\n' "$sum" > "$dir/go.txt"
[[ "$(read_header "$dir/go.txt")" == "$sum" ]] || fail "go canonical header"

printf 'HTTP/2 200\r\nx-sha256: %s\r\n' "$sum" > "$dir/h2.txt"
[[ "$(read_header "$dir/h2.txt")" == "$sum" ]] || fail "http2 lower header"

printf 'X-SHA256: %s\n' "$sum" > "$dir/upper.txt"
[[ "$(read_header "$dir/upper.txt")" == "$sum" ]] || fail "upper header"

printf 'HTTP/1.1 200 OK\r\nContent-Length: 10\r\n' > "$dir/none.txt"
[[ -z "$(read_header "$dir/none.txt")" ]] || fail "missing header should be empty"

printf 'X-Sha256: aaaa\r\nX-Sha256: %s\r\n' "$sum" > "$dir/last.txt"
[[ "$(read_header "$dir/last.txt")" == "$sum" ]] || fail "last header wins"

echo "ok"
