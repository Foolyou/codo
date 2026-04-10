# codo

`codo` is a Go-based personal coding assistant runtime that keeps the assistant inside a long-lived rootless Docker container while keeping upstream model credentials and audit persistence on the host.

## Architecture

- The assistant runtime lives in a long-lived Docker container.
- Only one explicit workspace is mounted into the container, at `/workspace`.
- The container reaches host control services through Unix domain sockets mounted at `/run/codo`.
- The host-side model proxy injects the Bailian API key and writes append-only JSONL request audit logs.
- The container-side bash wrapper emits start and completion audit events before and after every assistant shell command.

## Repository Layout

- `cmd/codo/main.go`: CLI entrypoint for bootstrap, host, and container commands.
- `internal/bootstrap/bootstrap.go`: unified `codo up` orchestration.
- `internal/controlplane/proxy.go`: Bailian OpenAI-compatible model proxy with host-side credential injection and JSONL audit logs.
- `internal/controlplane/audit.go`: host-side bash audit collector.
- `internal/runtime/docker.go`: runtime lifecycle tooling and generated Docker container spec.
- `internal/runtime/bash.go`: audited container-side bash wrapper and model proxy client.
- `examples/runtime-config.example.json`: starter template for copied custom configs.

## Prerequisites

- Go 1.26+
- Rootless Docker
- A Bailian API key exported on the host as `BAILIAN_API_KEY`

## Default Home Layout

By default, config-backed `codo` commands resolve config in this order:

1. `--config <path>`
2. `CODO_CONFIG`
3. `CODO_HOME/config/runtime.json`
4. `$HOME/.codo/config/runtime.json`

The default home layout is:

- `~/.codo/config/runtime.json`: runtime config file
- `~/.codo/workspace/`: default workspace mounted into the container
- `~/.codo/config/state/`: runtime state, sockets, and logs

You can move the root with `CODO_HOME`:

```bash
CODO_HOME=/srv/codo ./codo up
```

## Quick Start

Build the binary from the repository root:

```bash
go build ./cmd/codo
```

Export the provider credential on the host:

```bash
export BAILIAN_API_KEY=your-key-here
```

Bring up the control plane and runtime together:

```bash
./codo up
```

On first run, `codo up` will create:

- `~/.codo/config/runtime.json`
- `~/.codo/workspace/`

It will also build the runtime image if the configured image is missing locally, then start the host control plane and the long-lived assistant container. The command stays in the foreground until interrupted.

## Low-Level Commands

The low-level commands still work and use the same config discovery rules:

```bash
./codo control-plane serve
./codo runtime build-image
./codo runtime start
./codo runtime status
./codo runtime exec "pwd && ls -la"
./codo runtime reconnect
./codo runtime rebuild
```

Use `--config` when you want a specific file:

```bash
./codo runtime status --config /path/to/runtime.json
```

## Custom Configs

If you want a managed custom config instead of the default home layout, point `--config` or `CODO_CONFIG` at an existing file. `codo up` only auto-creates the default home config; it will fail for a missing custom path instead of writing files implicitly.

The repository template at `examples/runtime-config.example.json` is intended to be copied into a config directory such as `~/.codo/config/runtime.json` or `/srv/codo/config/runtime.json`. Its relative paths are designed to resolve correctly after copying.

## Stable Paths

The default starter config resolves to these paths:

- Workspace mount inside the container: `/workspace`
- Model proxy socket inside the container: `/run/codo/model-proxy.sock`
- Audit collector socket inside the container: `/run/codo/audit.sock`
- Host-side bash audit log: `~/.codo/config/state/logs/bash-audit.jsonl`
- Host-side model proxy audit log: `~/.codo/config/state/logs/model-proxy.jsonl`

## Rebuild Flow

Rebuilding removes and recreates the container while preserving:

- The mounted workspace on the host
- The host-side JSONL audit logs
- The persisted `runtime_instance_id` in `runtime-instance.json`

Run:

```bash
./codo runtime rebuild
```

This creates a fresh container from the image without depending on any extra long-lived runtime-state volume inside Docker.

## Workspace Selection

- `runtime.workspace_path` is the only project directory mounted into the container.
- The implementation does not mount the full home directory, SSH config, or other broad host paths.
- The workspace is always exposed inside the container at `runtime.workspace_mount_path`, which defaults to `/workspace`.

## Audit Logs

Bash audit records are append-only JSONL entries. Each completed record includes:

- `exec_id`
- `runtime_instance_id`
- `session_id`
- `container_id`
- `workspace_id`
- `command`
- `cwd`
- `started_at`
- `ended_at`
- `exit_code`
- `stdout_preview`
- `stderr_preview`
- `stdout_bytes`
- `stderr_bytes`
- `stdout_sha256`
- `stderr_sha256`
- `stdout_truncated`
- `stderr_truncated`

Inspect recent bash audit records:

```bash
tail -n 20 ~/.codo/config/state/logs/bash-audit.jsonl
```

Inspect recent model proxy records:

```bash
tail -n 20 ~/.codo/config/state/logs/model-proxy.jsonl
```

## Model Proxy Usage

Inside the container, assistant requests should go through the mounted Unix socket instead of directly to Bailian. The helper command is:

```bash
codo runtime proxy-request --method POST --path /v1/chat/completions --body-file request.json
```

The container does not receive the Bailian API key or upstream base URL. The host-side proxy injects the credential when forwarding the request.

## Validation

The repository includes tests for:

- Explicit workspace-only container mounts and absence of upstream credentials in container env
- Runtime identity persistence across reloads
- Fail-closed bash execution when the audit collector is unavailable
- Host-side audit record persistence
- Host-side proxy credential injection and request audit logging
- Config discovery precedence and default-home bootstrap behavior
- Unified `codo up` orchestration and CLI compatibility with low-level commands

Run:

```bash
GOCACHE=/tmp/codo-gocache GOPATH=/tmp/codo-gopath GOMODCACHE=/tmp/codo-gomodcache go test ./...
```

Run the end-to-end Docker smoke test:

```bash
make docker-smoke
```

The smoke test:

- builds `./bin/codo`
- starts a temporary host control plane with temporary Unix sockets
- builds the runtime image
- creates a temporary runtime container with a unique name
- executes an audited command inside the container
- verifies the command output and the host-side audit log
- verifies the provider credential is not present in the container env
- cleans everything up automatically

If `BAILIAN_API_KEY` is unset, the smoke test uses a unique dummy value because it does not send a real upstream model request.
