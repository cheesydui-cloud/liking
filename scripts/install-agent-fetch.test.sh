#!/usr/bin/env bash
# Exercises agent download fallback without root or systemd.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }

make_elf() {
  python3 - "$1" <<'PY'
import sys
path = sys.argv[1]
open(path, "wb").write(bytes.fromhex("7f454c46") + b"\0" * (1024 * 1024 + 8))
PY
}

FIX="$(mktemp -d)"
trap 'rm -rf "$FIX"' EXIT
make_elf "$FIX/amd64-bytes"
make_elf "$FIX/arm64-bytes"
printf 'amd64' >> "$FIX/amd64-bytes"
printf 'arm64' >> "$FIX/arm64-bytes"

hash_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

{
  echo "$(hash_of "$FIX/amd64-bytes")  liking-agent-linux-amd64"
  echo "$(hash_of "$FIX/amd64-bytes")  liking-agent"
  echo "$(hash_of "$FIX/arm64-bytes")  liking-agent-linux-arm64"
} > "$FIX/SHA256SUMS"

run_sourced() {
  local mode="$1"
  env \
    LIKING_INSTALL_TEST=1 \
    LIKING_TEST_ROOT="$ROOT" \
    LIKING_TEST_FIX="$FIX" \
    LIKING_TEST_MODE="$mode" \
    FAIL_AMD64="${FAIL_AMD64:-}" \
    FAIL_ALIAS="${FAIL_ALIAS:-}" \
    FAIL_ARM64="${FAIL_ARM64:-}" \
    bash --noprofile --norc -c '
      set -euo pipefail
      if ! command -v sha256sum >/dev/null 2>&1; then
        sha256sum() { shasum -a 256 "$1" | awk "{print \$1\"  \"\$2}"; }
      fi
      # shellcheck disable=SC1091
      source "$LIKING_TEST_ROOT/install.sh"
      HOST_GOARCH=amd64
      curl() {
        local dest="" arg url prev=""
        for arg in "$@"; do
          if [[ "$prev" == "-o" ]]; then dest="$arg"; fi
          prev="$arg"
          url="$arg"
        done
        case "$url" in
          */liking-agent-linux-amd64)
            if [[ "${FAIL_AMD64:-}" == 1 ]]; then echo "curl: amd64 asset refused" >&2; return 22; fi
            cp "$LIKING_TEST_FIX/amd64-bytes" "$dest" ;;
          */liking-agent)
            if [[ "${FAIL_ALIAS:-}" == 1 ]]; then echo "curl: alias refused" >&2; return 22; fi
            cp "$LIKING_TEST_FIX/amd64-bytes" "$dest" ;;
          */liking-agent-linux-arm64)
            if [[ "${FAIL_ARM64:-}" == 1 ]]; then echo "curl: arm64 asset refused" >&2; return 22; fi
            cp "$LIKING_TEST_FIX/arm64-bytes" "$dest" ;;
          *) echo "unexpected url $url" >&2; return 1 ;;
        esac
      }
      dir="$(mktemp -d)"
      cp "$LIKING_TEST_FIX/SHA256SUMS" "$dir/SHA256SUMS"
      if [[ "$LIKING_TEST_MODE" == "die" ]]; then
        fetch_release_agents "https://example.test/v0" "$dir"
        exit 0
      fi
      fetch_release_agents "https://example.test/v0" "$dir"
      [[ -f "$dir/liking-agent-linux-amd64" ]] || { echo "missing amd64 agent"; exit 1; }
      is_elf "$dir/liking-agent-linux-amd64" || { echo "not elf"; exit 1; }
      match_sha "$dir/liking-agent-linux-amd64" "liking-agent-linux-amd64" "$dir/SHA256SUMS" \
        || match_sha "$dir/liking-agent-linux-amd64" "liking-agent" "$dir/SHA256SUMS" \
        || { echo "checksum mismatch"; exit 1; }
      if [[ "$LIKING_TEST_MODE" == "with-arm" ]]; then
        [[ -f "$dir/liking-agent-linux-arm64" ]] || { echo "missing arm64 agent"; exit 1; }
      fi
      if [[ "$LIKING_TEST_MODE" == "no-arm" ]]; then
        [[ ! -f "$dir/liking-agent-linux-arm64" ]] || { echo "arm64 should be absent"; exit 1; }
      fi
    '
}

echo "case: direct amd64"
FAIL_AMD64=0 FAIL_ALIAS=1 FAIL_ARM64=0
run_sourced with-arm

echo "case: amd64 URL fails, alias used"
FAIL_AMD64=1 FAIL_ALIAS=0 FAIL_ARM64=1
run_sourced no-arm

echo "case: host arch missing aborts"
FAIL_AMD64=1 FAIL_ALIAS=1 FAIL_ARM64=0
if run_sourced die; then
  fail "expected abort when both amd64 URLs fail"
fi

echo "ok"
