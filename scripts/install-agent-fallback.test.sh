#!/usr/bin/env bash
# Panel download fails; the install script must take the GitHub mirror.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
script="$ROOT/internal/installscript/install-agent.sh"
fail() { echo "FAIL: $*" >&2; exit 1; }

if ! command -v sha256sum >/dev/null 2>&1; then
  sha256sum() { shasum -a 256 "$1" | awk '{print $1"  "$2}'; }
  export -f sha256sum
fi

FIX="$(mktemp -d)"
trap 'rm -rf "$FIX"' EXIT

python3 - "$FIX/agent" <<'PY'
import sys
open(sys.argv[1], "wb").write(bytes.fromhex("7f454c46") + b"\0" * (1024 * 1024 + 8))
PY
sum="$(sha256sum "$FIX/agent" | awk '{print $1}')"
printf '%s\n' "$sum" > "$FIX/sum"
{
  echo "$sum  liking-agent-linux-amd64"
  echo "$sum  liking-agent"
} > "$FIX/SHA256SUMS"

cat > "$FIX/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
dest="" hdr="" url="" prev=""
for arg in "$@"; do
  if [[ "$prev" == "-o" ]]; then dest="$arg"; fi
  if [[ "$prev" == "-D" ]]; then hdr="$arg"; fi
  prev="$arg"
  case "$arg" in
    http://*|https://*) url="$arg" ;;
  esac
done
printf '%s\n' "$url" >> "${LIKING_CURL_LOG:?}"
case "$url" in
  *://panel.test/v1/agent-bin*)
    if [[ "${FAIL_PANEL:-}" == 1 ]]; then
      echo "curl: panel refused" >&2
      exit 28
    fi
    cp "${LIKING_TEST_FIX:?}/agent" "$dest"
    printf 'HTTP/1.1 200 OK\r\nX-Sha256: %s\r\n' "$(cat "${LIKING_TEST_FIX:?}/sum")" > "$hdr"
    ;;
  */SHA256SUMS)
    if [[ "${FAIL_SUMS:-}" == 1 ]]; then exit 22; fi
    cp "${LIKING_TEST_FIX:?}/SHA256SUMS" "$dest"
    ;;
  */liking-agent-linux-amd64)
    if [[ "${FAIL_ASSET:-}" == 1 ]]; then exit 22; fi
    cp "${LIKING_TEST_FIX:?}/agent" "$dest"
    ;;
  */liking-agent)
    if [[ "${FAIL_ALIAS:-}" == 1 ]]; then exit 22; fi
    cp "${LIKING_TEST_FIX:?}/agent" "$dest"
    ;;
  *)
    echo "unexpected url $url" >&2
    exit 1
    ;;
esac
SH
chmod +x "$FIX/curl"

run_fetch() {
  local dest="$FIX/out-agent"
  rm -f "$dest" "$FIX/curl.log"
  env \
    PATH="$FIX:$PATH" \
    LIKING_AGENT_FETCH_TEST=1 \
    LIKING_FETCH_DELAY=0 \
    LIKING_PANEL_URL="${LIKING_PANEL_URL:-http://panel.test}" \
    LIKING_GITHUB_PROXY="${LIKING_GITHUB_PROXY:-}" \
    LIKING_TEST_VERSION="${LIKING_TEST_VERSION:-0.3.44}" \
    LIKING_TEST_DEST="$dest" \
    LIKING_CURL_LOG="$FIX/curl.log" \
    LIKING_TEST_FIX="$FIX" \
    LIKING_TEST_SUM="$sum" \
    FAIL_PANEL="${FAIL_PANEL:-}" \
    FAIL_SUMS="${FAIL_SUMS:-}" \
    FAIL_ASSET="${FAIL_ASSET:-}" \
    FAIL_ALIAS="${FAIL_ALIAS:-}" \
    bash --noprofile --norc "$script"
}

echo "case: panel download"
FAIL_PANEL=0 FAIL_SUMS=0 FAIL_ASSET=0 FAIL_ALIAS=1
LIKING_GITHUB_PROXY=""
run_fetch
grep -q 'panel.test/v1/agent-bin' "$FIX/curl.log" || fail "did not try panel"
if grep -q 'github.com' "$FIX/curl.log"; then
  fail "github used when panel worked"
fi
[[ -f "$FIX/out-agent" ]] || fail "missing agent"

echo "case: panel down, mirror used"
FAIL_PANEL=1 FAIL_SUMS=0 FAIL_ASSET=0 FAIL_ALIAS=1
LIKING_GITHUB_PROXY="https://gh-proxy.test/"
run_fetch
grep -q 'https://gh-proxy.test/https://github.com/cheesydui-cloud/liking/releases/download/v0.3.44/SHA256SUMS' "$FIX/curl.log" || fail "sums url"
grep -q 'https://gh-proxy.test/https://github.com/cheesydui-cloud/liking/releases/download/v0.3.44/liking-agent-linux-amd64' "$FIX/curl.log" || fail "asset url"
[[ -f "$FIX/out-agent" ]] || fail "missing mirrored agent"

echo "case: arch asset fails, alias used"
FAIL_PANEL=1 FAIL_SUMS=0 FAIL_ASSET=1 FAIL_ALIAS=0
LIKING_GITHUB_PROXY="https://gh-proxy.test/"
run_fetch
grep -q '/liking-agent$' "$FIX/curl.log" || fail "alias not tried"
[[ -f "$FIX/out-agent" ]] || fail "missing alias agent"

echo "case: panel down without mirror"
FAIL_PANEL=1
LIKING_GITHUB_PROXY=""
if run_fetch >"$FIX/stdout" 2>"$FIX/stderr"; then
  fail "expected failure without mirror"
fi
grep -q -- '--gh-proxy' "$FIX/stderr" || fail "missing gh-proxy hint: $(cat "$FIX/stderr")"

echo "ok"
