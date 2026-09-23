#!/usr/bin/env bash
# Fixture checks for scripts/check-release-digests.sh. No network.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CHECK="$ROOT/scripts/check-release-digests.sh"
fail() { echo "FAIL: $*" >&2; exit 1; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

hash_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

printf 'agent-bytes\n' > "$tmp/agent"
printf 'server-bytes\n' > "$tmp/server"
agent="$(hash_of "$tmp/agent")"
server="$(hash_of "$tmp/server")"
cat > "$tmp/SHA256SUMS" <<EOF
${agent}  liking-agent-linux-amd64
${agent}  liking-agent
${server}  liking-server-linux-amd64
EOF
sums="$(hash_of "$tmp/SHA256SUMS")"

cat > "$tmp/digests-ok" <<EOF
${agent}  liking-agent-linux-amd64
${agent}  liking-agent
${server}  liking-server-linux-amd64
${sums}  SHA256SUMS
EOF

echo "case: matching digests"
DIGEST_FILE="$tmp/digests-ok" bash "$CHECK" "$tmp/SHA256SUMS" >/dev/null

echo "case: amd64 agent digest differs"
cat > "$tmp/digests-bad" <<EOF
deadbeef  liking-agent-linux-amd64
deadbeef  liking-agent
${server}  liking-server-linux-amd64
${sums}  SHA256SUMS
EOF
set +e
DIGEST_FILE="$tmp/digests-bad" bash "$CHECK" "$tmp/SHA256SUMS" >/dev/null
rc=$?
set -e
[[ "$rc" == 2 ]] || fail "expected exit 2 for a mismatched agent, got ${rc}"

echo "case: digest not published yet"
cat > "$tmp/digests-pending" <<EOF
${agent}  liking-agent
${server}  liking-server-linux-amd64
${sums}  SHA256SUMS
EOF
set +e
DIGEST_FILE="$tmp/digests-pending" bash "$CHECK" "$tmp/SHA256SUMS" >/dev/null
rc=$?
set -e
[[ "$rc" == 3 ]] || fail "expected exit 3 when a digest is missing, got ${rc}"

echo "ok"
