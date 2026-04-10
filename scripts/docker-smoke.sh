#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: ./scripts/docker-smoke.sh [--workspace <path>] [--image <name>] [--binary <path>] [--keep-artifacts]

Runs an end-to-end Docker smoke test for the codo runtime using:
- a temporary runtime name
- a temporary host state directory
- a temporary config file

Environment variables:
- BAILIAN_API_KEY: Optional. If unset, the smoke test uses a unique dummy value because it does not send a real model request upstream.
- CODO_SMOKE_WORKSPACE: Optional default for --workspace.
- CODO_SMOKE_IMAGE: Optional default for --image.
- CODO_SMOKE_BINARY: Optional default for --binary.
- CODO_SMOKE_KEEP_ARTIFACTS=1: Keep the temp config, logs, and state directory after the run.
EOF
}

log() {
  printf '[docker-smoke] %s\n' "$*"
}

fail() {
  log "error: $*"
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKSPACE_PATH="${CODO_SMOKE_WORKSPACE:-$REPO_ROOT}"
IMAGE_NAME="${CODO_SMOKE_IMAGE:-codo:latest}"
BINARY_PATH="${CODO_SMOKE_BINARY:-$REPO_ROOT/bin/codo}"
KEEP_ARTIFACTS="${CODO_SMOKE_KEEP_ARTIFACTS:-0}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --workspace)
      [[ $# -ge 2 ]] || fail "--workspace requires a path"
      WORKSPACE_PATH="$2"
      shift 2
      ;;
    --image)
      [[ $# -ge 2 ]] || fail "--image requires a name"
      IMAGE_NAME="$2"
      shift 2
      ;;
    --binary)
      [[ $# -ge 2 ]] || fail "--binary requires a path"
      BINARY_PATH="$2"
      shift 2
      ;;
    --keep-artifacts)
      KEEP_ARTIFACTS=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

WORKSPACE_PATH="$(cd "$WORKSPACE_PATH" && pwd)"
case "$BINARY_PATH" in
  /*) ;;
  *) BINARY_PATH="$REPO_ROOT/$BINARY_PATH" ;;
esac

require_command docker
require_command go
require_command mktemp

TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/codo-docker-smoke.XXXXXX")"
RUNTIME_NAME="codo-smoke-$(date +%s)-$$"
SMOKE_MARKER="smoke-$(date +%s)-$$"
STATE_DIR="$TEMP_DIR/state"
CONFIG_PATH="$TEMP_DIR/runtime-config.json"
CONTROL_PLANE_LOG="$TEMP_DIR/control-plane.log"
AUDIT_LOG="$STATE_DIR/logs/bash-audit.jsonl"
API_KEY_VALUE="${BAILIAN_API_KEY:-dummy-${SMOKE_MARKER}}"
CONTROL_PLANE_PID=""

cleanup() {
  local exit_code=$?
  set +e

  if [[ -x "$BINARY_PATH" && -f "$CONFIG_PATH" ]]; then
    "$BINARY_PATH" runtime stop --config "$CONFIG_PATH" >/dev/null 2>&1 || true
  fi

  docker rm -f "$RUNTIME_NAME" >/dev/null 2>&1 || true

  if [[ -n "$CONTROL_PLANE_PID" ]]; then
    kill "$CONTROL_PLANE_PID" >/dev/null 2>&1 || true
    wait "$CONTROL_PLANE_PID" >/dev/null 2>&1 || true
  fi

  if [[ "$KEEP_ARTIFACTS" == "1" ]]; then
    log "kept smoke artifacts in $TEMP_DIR"
  else
    rm -rf "$TEMP_DIR"
  fi

  exit "$exit_code"
}

trap cleanup EXIT

wait_for_socket() {
  local path=$1
  local name=$2

  for _ in $(seq 1 100); do
    if [[ -S "$path" ]]; then
      return 0
    fi

    if [[ -n "$CONTROL_PLANE_PID" ]] && ! kill -0 "$CONTROL_PLANE_PID" >/dev/null 2>&1; then
      log "control-plane exited early while waiting for $name"
      [[ -f "$CONTROL_PLANE_LOG" ]] && cat "$CONTROL_PLANE_LOG"
      return 1
    fi

    sleep 0.1
  done

  log "timed out waiting for $name at $path"
  [[ -f "$CONTROL_PLANE_LOG" ]] && cat "$CONTROL_PLANE_LOG"
  return 1
}

wait_for_container_running() {
  local status=""

  for _ in $(seq 1 100); do
    status="$(docker inspect "$RUNTIME_NAME" --format '{{.State.Status}}' 2>/dev/null || true)"
    if [[ "$status" == "running" ]]; then
      return 0
    fi
    sleep 0.1
  done

  log "container did not reach running state, last status: ${status:-missing}"
  docker logs "$RUNTIME_NAME" 2>/dev/null || true
  return 1
}

cat >"$CONFIG_PATH" <<EOF
{
  "runtime": {
    "name": "$RUNTIME_NAME",
    "image": "$IMAGE_NAME",
    "workspace_path": "$WORKSPACE_PATH",
    "workspace_label": "smoke-workspace",
    "workspace_mount_path": "/workspace",
    "host_state_dir": "$STATE_DIR",
    "container_control_dir": "/run/codo"
  },
  "provider": {
    "type": "bailian-openai-compatible",
    "base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1",
    "api_key_env": "BAILIAN_API_KEY"
  },
  "proxy": {},
  "audit": {
    "preview_bytes": 4096
  }
}
EOF

mkdir -p "$(dirname "$BINARY_PATH")"
BINARY_PATH="$(cd "$(dirname "$BINARY_PATH")" && pwd)/$(basename "$BINARY_PATH")"
export GOCACHE="${GOCACHE:-$TEMP_DIR/go-build}"
export GOPATH="${GOPATH:-$TEMP_DIR/go}"
export GOMODCACHE="${GOMODCACHE:-$TEMP_DIR/go/pkg/mod}"

log "building codo binary at $BINARY_PATH"
(
  cd "$REPO_ROOT"
  go build -o "$BINARY_PATH" ./cmd/codo
)

log "starting control-plane"
(
  cd "$REPO_ROOT"
  BAILIAN_API_KEY="$API_KEY_VALUE" "$BINARY_PATH" control-plane serve --config "$CONFIG_PATH" >"$CONTROL_PLANE_LOG" 2>&1
) &
CONTROL_PLANE_PID=$!

wait_for_socket "$STATE_DIR/run/audit.sock" "audit socket"
wait_for_socket "$STATE_DIR/run/model-proxy.sock" "model proxy socket"

log "building runtime image $IMAGE_NAME"
(
  cd "$REPO_ROOT"
  "$BINARY_PATH" runtime build-image --config "$CONFIG_PATH"
)

log "rebuilding isolated runtime $RUNTIME_NAME"
(
  cd "$REPO_ROOT"
  "$BINARY_PATH" runtime rebuild --config "$CONFIG_PATH"
)

wait_for_container_running

log "running audited bash command inside container"
if ! command_output="$(
  cd "$REPO_ROOT"
  "$BINARY_PATH" runtime exec --config "$CONFIG_PATH" "printf '%s\n' '$SMOKE_MARKER' && pwd" 2>&1
)"; then
  printf '%s\n' "$command_output"
  docker logs "$RUNTIME_NAME" 2>/dev/null || true
  [[ -f "$CONTROL_PLANE_LOG" ]] && cat "$CONTROL_PLANE_LOG"
  fail "runtime exec failed during smoke test"
fi
printf '%s\n' "$command_output"

[[ "$command_output" == *"$SMOKE_MARKER"* ]] || fail "missing smoke marker in container output"
[[ "$command_output" == *"/workspace"* ]] || fail "missing /workspace in container output"

container_env="$(docker inspect "$RUNTIME_NAME" --format '{{json .Config.Env}}')"
[[ "$container_env" != *"$API_KEY_VALUE"* ]] || fail "container env leaked provider credential"

[[ -s "$AUDIT_LOG" ]] || fail "missing bash audit log at $AUDIT_LOG"
audit_line="$(tail -n 1 "$AUDIT_LOG")"
printf '%s\n' "$audit_line"

[[ "$audit_line" == *"$SMOKE_MARKER"* ]] || fail "bash audit log is missing the smoke marker"
[[ "$audit_line" == *'"exit_code":0'* ]] || fail "bash audit log did not record a successful exit code"
[[ "$audit_line" == *'"cwd":"/workspace"'* ]] || fail "bash audit log did not record /workspace as cwd"

log "docker smoke test passed"
