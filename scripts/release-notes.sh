#!/usr/bin/env bash
# 从 CHANGELOG.md 抽出指定版本的完整升级详情，供 GitHub Release 使用。
# 用法: scripts/release-notes.sh v0.1.0
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CHANGELOG="$ROOT/CHANGELOG.md"
VERSION="${1:-}"

if [[ -z "$VERSION" ]]; then
  echo "用法: $0 vX.Y.Z" >&2
  exit 1
fi

if [[ ! -f "$CHANGELOG" ]]; then
  echo "缺少 $CHANGELOG：每个版本必须先写升级详情再发版" >&2
  exit 1
fi

python3 - "$CHANGELOG" "$VERSION" <<'PY'
import re, sys
from pathlib import Path

path, version = Path(sys.argv[1]), sys.argv[2]
text = path.read_text(encoding="utf-8")
pat = re.compile(rf"^## {re.escape(version)}\b.*$", re.M)
m = pat.search(text)
if not m:
    sys.stderr.write(
        f"CHANGELOG.md 缺少「## {version}」章节。\n"
        "发版前必须把该版本的详细升级内容写进 CHANGELOG.md。\n"
    )
    sys.exit(1)

start = m.start()
nxt = re.search(r"^## v", text[m.end():], re.M)
end = m.end() + nxt.start() if nxt else len(text)
body = text[start:end].strip() + "\n"

lines = [ln.strip() for ln in body.splitlines() if ln.strip() and not ln.startswith("#")]
bullets = [ln for ln in lines if ln.startswith("- ") or ln.startswith("* ")]
if len(bullets) < 3:
    sys.stderr.write(
        f"CHANGELOG.md 中 {version} 的升级详情过少（至少 3 条 - 列表项）。\n"
        "请写清新增 / 修复 / 改进 / 升级注意后再发版。\n"
    )
    sys.exit(1)

sys.stdout.write(body)
PY
