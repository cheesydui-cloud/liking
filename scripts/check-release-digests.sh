#!/usr/bin/env bash
# Compare a sha256sum file to GitHub release asset digests.
# Usage: check-release-digests.sh <SHA256SUMS> <owner/repo> <tag>
# Set DIGEST_FILE to a "hash  name" file to skip the API (tests).
# Exit 0 when every line matches, 2 on a mismatch, 3 when a digest is missing.
set -euo pipefail

sums="${1:?SHA256SUMS path}"
repo="${2:-}"
tag="${3:-}"

tmp="$(mktemp)"
json=""
list=""
cleanup() { rm -f "$tmp" ${json:+"$json"} ${list:+"$list"}; }
trap cleanup EXIT

if [[ -n "${DIGEST_FILE:-}" ]]; then
  cp "$DIGEST_FILE" "$tmp"
else
  [[ -n "$repo" && -n "$tag" ]] || { echo "需要 owner/repo 和 tag" >&2; exit 1; }
  json="$(mktemp)"
  hdr=(-H "Accept: application/vnd.github+json" -H "User-Agent: liking-release-check")
  token="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
  if [[ -n "$token" ]]; then
    hdr+=(-H "Authorization: Bearer ${token}")
  fi
  # Draft releases 404 at /releases/tags/{tag}. List releases instead; an
  # authenticated token can see drafts, and that is what CI publishes first.
  code="$(curl -sS -o "$json" -w '%{http_code}' "${hdr[@]}" "https://api.github.com/repos/${repo}/releases/tags/${tag}" || true)"
  if [[ "$code" != "200" ]]; then
    list="$(mktemp)"
    curl -fsSL "${hdr[@]}" "https://api.github.com/repos/${repo}/releases?per_page=100" -o "$list"
    python3 - "$list" "$tag" "$json" <<'PY'
import json, sys
from pathlib import Path
rels = json.load(open(sys.argv[1], encoding="utf-8"))
tag = sys.argv[2]
match = next((r for r in rels if r.get("tag_name") == tag), None)
if match is None:
    sys.stderr.write(f"release not listed yet: {tag}\n")
    sys.exit(3)
Path(sys.argv[3]).write_text(json.dumps(match), encoding="utf-8")
PY
  fi
  python3 - "$json" > "$tmp" <<'PY'
import json
import sys

rel = json.load(open(sys.argv[1], encoding="utf-8"))
for asset in rel.get("assets") or []:
    digest = asset.get("digest") or ""
    if digest.startswith("sha256:"):
        digest = digest[7:]
    name = asset.get("name") or ""
    if name:
        print(f"{digest}  {name}")
PY
fi

hash_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

pending=0
bad=0
check_one() {
  local name="$1" sum="$2" got
  got="$(awk -v a="$name" '$2 == a { print $1; exit }' "$tmp")"
  got="${got#sha256:}"
  if [[ -z "$got" ]]; then
    echo "digest pending: ${name}" >&2
    pending=1
  elif [[ "$got" != "$sum" ]]; then
    echo "mismatch ${name}: github=${got} sums=${sum}" >&2
    bad=1
  else
    echo "ok ${name}"
  fi
}

while read -r sum name; do
  [[ -n "${sum:-}" && -n "${name:-}" ]] || continue
  check_one "$name" "$sum"
done < "$sums"

check_one "SHA256SUMS" "$(hash_of "$sums")"

if [[ "$bad" == 1 ]]; then
  exit 2
fi
if [[ "$pending" == 1 ]]; then
  exit 3
fi
exit 0
