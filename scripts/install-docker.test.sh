#!/usr/bin/env bash
# Checks the docker install script against the shipped image files.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

env \
  LIKING_INSTALL_TEST=1 \
  LIKING_TEST_ROOT="$ROOT" \
  LIKING_TEST_TMP="$tmp" \
  bash --noprofile --norc -c '
  set -euo pipefail
  # shellcheck disable=SC1091
  source "$LIKING_TEST_ROOT/install.sh"
  GH_PROXY=""
  PANEL_ADDR="127.0.0.1:8899"
  DOCKER_SOCK=1
  DOCKER_BOOTSTRAP=0
  managed_dockerfile > "$LIKING_TEST_TMP/Dockerfile"
  write_docker_compose "$LIKING_TEST_TMP/compose.yml"
  DOCKER_SOCK=0
  write_docker_compose "$LIKING_TEST_TMP/nosock.yml"
  DOCKER_SOCK=1
  PANEL_ADDR="0.0.0.0:8899"
  write_docker_compose "$LIKING_TEST_TMP/public.yml"
  MODE=update
  DOCKER_ACTION=install
  prefer_docker_mode 1 0
  printf "%s %s\n" "$MODE" "$DOCKER_ACTION" > "$LIKING_TEST_TMP/route-update"
  MODE=reset-password
  prefer_docker_mode 1 0
  printf "%s %s\n" "$MODE" "$DOCKER_ACTION" > "$LIKING_TEST_TMP/route-reset"
  MODE=uninstall
  prefer_docker_mode 1 0
  printf "%s %s\n" "$MODE" "$DOCKER_ACTION" > "$LIKING_TEST_TMP/route-uninstall"
  MODE=update
  DOCKER_ACTION=install
  prefer_docker_mode 1 1
  printf "%s %s\n" "$MODE" "$DOCKER_ACTION" > "$LIKING_TEST_TMP/route-systemd"
  MODE=server
  prefer_docker_mode 0 0
  printf "%s\n" "$MODE" > "$LIKING_TEST_TMP/route-plain"
'

diff -u "$ROOT/deploy/docker/Dockerfile" "$tmp/Dockerfile" || fail "Dockerfile drifted from install.sh"
diff -u "$ROOT/deploy/docker/docker-compose.yml" "$tmp/compose.yml" || fail "compose drifted from install.sh"
grep -q "docker.sock" "$tmp/nosock.yml" && fail "no-sock still mounts docker.sock"
grep -q "/opt/liking-docker:/opt/liking-docker" "$tmp/nosock.yml" && fail "no-sock still mounts the build dir"
grep -q "LIKING_TRUST_DOCKER_GATEWAY" "$tmp/public.yml" && fail "public bind trusts forwarded headers"
grep -q "0.0.0.0:8899:8899" "$tmp/public.yml" || fail "public bind port missing"
[[ "$(cat "$tmp/route-update")" == "docker update" ]] || fail "update did not switch to docker"
[[ "$(cat "$tmp/route-reset")" == "docker reset-password" ]] || fail "reset did not switch to docker"
[[ "$(cat "$tmp/route-uninstall")" == "docker uninstall" ]] || fail "uninstall did not switch to docker"
[[ "$(cat "$tmp/route-systemd")" == "update install" ]] || fail "systemd install was switched to docker"
[[ "$(cat "$tmp/route-plain")" == "server" ]] || fail "plain install was switched to docker"
reset_fn="$(awk '/^do_docker_reset\(\)/,/^do_docker\(\)/' "$ROOT/install.sh")"
printf '%s\n' "$reset_fn" | grep -q -- "--reset-admin-password-file" || fail "docker reset does not read the password from a file"
printf '%s\n' "$reset_fn" | grep -q -- "--reset-admin-password " && fail "docker reset still passes the password as an argument"

echo "install-docker: OK"
