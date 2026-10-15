#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
TEST_DIR=$(mktemp -d)
trap 'rm -rf -- "$TEST_DIR"' EXIT
command -v jq >/dev/null || { echo "缺少命令: jq" >&2; exit 3; }

VERSION=v1.2.3
REGISTRY=127.0.0.1:5000
BACKEND_DIGEST=$(printf 'a%.0s' {1..64})
DEPLOY_DIR=$TEST_DIR/candidate
mkdir -p "$TEST_DIR/bin" "$TEST_DIR/old/data" "$TEST_DIR/shared" "$TEST_DIR/docker-config"
printf 'old-state\n' >"$TEST_DIR/old/data/sentinel"
printf 'shared-state\n' >"$TEST_DIR/shared/sentinel"
printf 'COINSPHERE_AUTH__SECRET_KEY=synthetic-test-secret\n' >"$TEST_DIR/runtime.env"
cat >"$TEST_DIR/release-manifest.json" <<EOF
{"version":"$VERSION","commit":"0123456789abcdef0123456789abcdef01234567","backendImage":"$REGISTRY/coinsphere/backend:$VERSION","backendDigest":"$REGISTRY/coinsphere/backend@sha256:$BACKEND_DIGEST"}
EOF

cat >"$TEST_DIR/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf '%s\n' "$*" >>"$DEPLOY_DOCKER_LOG"
if [[ ${1:-} == compose ]]; then
  shift
  while [[ ${1:-} == --project-name || ${1:-} == --project-directory || ${1:-} == --env-file || ${1:-} == -f ]]; do
    shift 2
  done
  # 只解析 Compose 命令，避免把 migration 的方向参数识别为服务操作。
  case "${1:-}" in
    stop|pull|rm|up) [[ ${!#} == backend ]] || exit 97 ;;
    run) [[ $* == 'run --rm --no-deps backend '* ]] || exit 97 ;;
    ps|logs) ;;
    *) exit 97 ;;
  esac
  if [[ ${1:-} == ps ]]; then
    printf 'backend\nworker\nweb\npostgresql\nsub2api\n'
  fi
  if [[ ${MOCK_FAIL:-} == migration && $* == *'/app/coinsphere-migrate'* ]]; then
    exit 42
  fi
elif [[ ${1:-} != run || $* != *'--network none'* ]]; then
  exit 97
fi
EOF
cat >"$TEST_DIR/bin/curl" <<'EOF'
#!/usr/bin/env bash
[[ ${MOCK_FAIL:-} != health ]] || exit 22
EOF
chmod +x "$TEST_DIR/bin/docker" "$TEST_DIR/bin/curl"
export PATH="$TEST_DIR/bin:$PATH"
export DOCKER_CONFIG="$TEST_DIR/docker-config"
export COINSPHERE_DEPLOY_DIR="$DEPLOY_DIR"
export COINSPHERE_RUNTIME_ENV_FILE="$TEST_DIR/runtime.env"
export COINSPHERE_REGISTRY=$REGISTRY
export COINSPHERE_DATABASE_PASSWORD_FILE="$TEST_DIR/database-password"
printf 'synthetic_database_password_1234567890\n' >"$COINSPHERE_DATABASE_PASSWORD_FILE"
chmod 0644 "$COINSPHERE_DATABASE_PASSWORD_FILE"
export COINSPHERE_COMPOSE_PROJECT=coinsphere-candidate
export COINSPHERE_DATABASE_NAME=coinsphere_candidate
export COINSPHERE_DATABASE_USER=coinsphere_candidate
export COINSPHERE_WEB_PORT=18081
export DEPLOY_DOCKER_LOG="$TEST_DIR/docker.log"

deploy() {
  bash "$ROOT_DIR/deploy/production/deploy.sh" "$VERSION" "$TEST_DIR/release-manifest.json"
}
assert_isolated() {
  ! grep -Eq 'down|--remove-orphans|(^| )volume( |$)|(stop|rm -f|pull|up).* (worker|web|postgresql|sub2api)( |$)' "$DEPLOY_DOCKER_LOG"
  test "$(cat "$TEST_DIR/old/data/sentinel")" = old-state
  test "$(cat "$TEST_DIR/shared/sentinel")" = shared-state
}

# A readable-by-others password file fails before any Docker operation.
status=0
deploy >"$TEST_DIR/insecure-password.log" 2>&1 || status=$?
test "$status" -eq 3
test ! -e "$DEPLOY_DOCKER_LOG"
chmod 0600 "$COINSPHERE_DATABASE_PASSWORD_FILE"
deploy >"$TEST_DIR/fresh.log" 2>&1
grep -Fxq "COINSPHERE_BACKEND_IMAGE=$REGISTRY/coinsphere/backend@sha256:$BACKEND_DIGEST" "$DEPLOY_DIR/.env"
grep -Fxq 'COINSPHERE_DATABASE_NAME=coinsphere_candidate' "$DEPLOY_DIR/.env"
cmp "$TEST_DIR/runtime.env" "$DEPLOY_DIR/runtime.env"
grep -Fq -- '--project-name coinsphere-candidate' "$DEPLOY_DOCKER_LOG"
grep -Fq 'run --rm --no-deps backend /app/coinsphere-migrate -config /app/config.yml -direction up' "$DEPLOY_DOCKER_LOG"
grep -Fq 'up -d --no-deps --wait --wait-timeout 180 backend' "$DEPLOY_DOCKER_LOG"
assert_isolated

# Saved identity, port and password survive an ordinary upgrade.
cp "$DEPLOY_DIR/.env" "$TEST_DIR/saved.env"
cp "$DEPLOY_DIR/compose.yaml" "$TEST_DIR/saved.compose"
unset COINSPHERE_COMPOSE_PROJECT COINSPHERE_DATABASE_NAME COINSPHERE_DATABASE_USER COINSPHERE_WEB_PORT
rm "$COINSPHERE_DATABASE_PASSWORD_FILE"
deploy >"$TEST_DIR/upgrade.log" 2>&1
cmp "$TEST_DIR/saved.env" "$DEPLOY_DIR/.env"
grep -Fq 'stop backend' "$DEPLOY_DOCKER_LOG"
assert_isolated

# Health failure restores the previous env/Compose and only restarts backend.
printf '\n# previous compose marker\n' >>"$DEPLOY_DIR/compose.yaml"
cp "$DEPLOY_DIR/compose.yaml" "$TEST_DIR/saved.compose"
: >"$DEPLOY_DOCKER_LOG"
status=0
MOCK_FAIL=health deploy >"$TEST_DIR/failed-upgrade.log" 2>&1 || status=$?
test "$status" -eq 22
cmp "$TEST_DIR/saved.env" "$DEPLOY_DIR/.env"
cmp "$TEST_DIR/saved.compose" "$DEPLOY_DIR/compose.yaml"
grep -Fq 'rm -f backend' "$DEPLOY_DOCKER_LOG"
test "$(grep -Fc 'up -d --no-deps --wait --wait-timeout 180 backend' "$DEPLOY_DOCKER_LOG")" -eq 2
assert_isolated

# A failed empty-db initialization leaves old/shared resources and its data directory intact.
export COINSPHERE_DEPLOY_DIR="$TEST_DIR/failed-candidate"
printf 'synthetic_database_password_1234567890\n' >"$COINSPHERE_DATABASE_PASSWORD_FILE"
chmod 0600 "$COINSPHERE_DATABASE_PASSWORD_FILE"
: >"$DEPLOY_DOCKER_LOG"
status=0
MOCK_FAIL=migration deploy >"$TEST_DIR/failed-fresh.log" 2>&1 || status=$?
test "$status" -eq 42
test ! -e "$COINSPHERE_DEPLOY_DIR/.env"
test -d "$COINSPHERE_DEPLOY_DIR/data/backend"
assert_isolated

export COINSPHERE_DEPLOY_DIR="$TEST_DIR/rejected"
printf '{"version":"v1.2.3","backendDigest":"latest"}\n' >"$TEST_DIR/invalid-manifest.json"
status=0
bash "$ROOT_DIR/deploy/production/deploy.sh" "$VERSION" "$TEST_DIR/invalid-manifest.json" >"$TEST_DIR/rejected.log" 2>&1 || status=$?
test "$status" -eq 2
test ! -e "$COINSPHERE_DEPLOY_DIR"

# Rebinding a deployed database fails before any Docker action.
export COINSPHERE_DEPLOY_DIR="$DEPLOY_DIR"
export COINSPHERE_DATABASE_NAME=other_database
: >"$DEPLOY_DOCKER_LOG"
status=0
deploy >"$TEST_DIR/rebind.log" 2>&1 || status=$?
test "$status" -eq 3
test ! -s "$DEPLOY_DOCKER_LOG"
cmp "$TEST_DIR/saved.env" "$DEPLOY_DIR/.env"
cmp "$TEST_DIR/saved.compose" "$DEPLOY_DIR/compose.yaml"

echo "固定 digest、候选隔离和定向失败恢复测试通过"
