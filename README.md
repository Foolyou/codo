# codo

`codo` is a Go-based personal coding assistant runtime that keeps the assistant inside a long-lived rootless Docker container while keeping upstream model credentials and audit persistence on the host.

## Architecture

- The assistant runtime lives in a long-lived Docker container.
- Only one explicit workspace is mounted into the container, at `/workspace`.
- The container reaches host control services through Unix domain sockets mounted at `/run/codo`.
- The host-side model proxy injects the Bailian API key and writes append-only JSONL request audit logs.
- The container-side bash wrapper emits start and completion audit events before and after every assistant shell command.

## Repository Layout

- `cmd/codo/main.go`: CLI entrypoint for host and container commands.
- `internal/controlplane/proxy.go`: Bailian OpenAI-compatible model proxy with host-side credential injection and JSONL audit logs.
- `internal/controlplane/audit.go`: host-side bash audit collector.
- `internal/runtime/docker.go`: runtime lifecycle tooling and generated Docker container spec.
- `internal/runtime/bash.go`: audited container-side bash wrapper and model proxy client.
- `examples/runtime-config.example.json`: example operator config.

## Prerequisites

- Go 1.26+
- Rootless Docker
- A Bailian API key exported on the host as `BAILIAN_API_KEY`

## Configuration

1. Copy `examples/runtime-config.example.json` to a local config file such as `runtime-config.json`.
2. Set `runtime.workspace_path` to the host directory you want mounted into the assistant container.
3. Adjust `runtime.host_state_dir` if you want runtime state and audit logs outside `./.runtime`.
4. Export the provider credential on the host:

```bash
export BAILIAN_API_KEY=your-key-here
```

The example config resolves to these stable paths by default:

- Workspace mount inside the container: `/workspace`
- Model proxy socket inside the container: `/run/codo/model-proxy.sock`
- Audit collector socket inside the container: `/run/codo/audit.sock`
- Host-side bash audit log: `./.runtime/logs/bash-audit.jsonl`
- Host-side model proxy audit log: `./.runtime/logs/model-proxy.jsonl`

## Build And Run

Build the binary:

```bash
go build ./cmd/codo
```

Start the host control plane:

```bash
./codo control-plane serve --config ./runtime-config.json
```

Build the runtime image:

```bash
./codo runtime build-image --config ./runtime-config.json
```

Create or start the long-lived runtime container:

```bash
./codo runtime start --config ./runtime-config.json
```

Run an audited shell command inside the container:

```bash
./codo runtime exec --config ./runtime-config.json "pwd && ls -la"
```

Reconnect to the running container:

```bash
./codo runtime reconnect --config ./runtime-config.json
```

## Rebuild Flow

Rebuilding removes and recreates the container while preserving:

- The mounted workspace on the host
- The host-side JSONL audit logs
- The persisted `runtime_instance_id` in `runtime-instance.json`

Run:

```bash
./codo runtime rebuild --config ./runtime-config.json
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
tail -n 20 ./.runtime/logs/bash-audit.jsonl
```

Inspect recent model proxy records:

```bash
tail -n 20 ./.runtime/logs/model-proxy.jsonl
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
