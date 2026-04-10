## Why

The current operator flow depends on a repo-local example config path and multiple manual startup steps, which makes deployment brittle and makes the runtime harder to access outside a development checkout. A stable user-home runtime root plus a single bootstrap command would give operators a predictable entrypoint without widening the assistant container's filesystem access.

## What Changes

- Add a default runtime home rooted at `~/.codo` for local operator deployments, with `config/` and `workspace/` as the well-known user-facing subdirectories.
- Add default config discovery so `codo` prefers an explicit `--config` flag, then an environment override, and otherwise falls back to `~/.codo/config/runtime.json`.
- Add a first-class bootstrap command that resolves the default config, prepares the default home layout when needed, starts the host control plane, and ensures the Docker runtime container is created or running from the same config.
- Define the default config template so the default mounted workspace lives under `~/.codo/workspace` and host-side runtime state remains inside the `.codo` root instead of depending on repo-relative paths.
- Update operator documentation to use the default `.codo` layout and the new single-command bootstrap flow for local deployment.

## Capabilities

### New Capabilities
- `runtime-bootstrap`: Define the default `~/.codo` home layout, config discovery rules, and a one-command operator entrypoint for bringing up the control plane and runtime container together.

### Modified Capabilities

None.

## Impact

- Affects CLI startup flow in `cmd/codo/main.go`
- Adds default home/config resolution and bootstrap helpers under `internal/config` and related runtime wiring
- Changes the default operator-facing configuration and documentation story away from repo-local example paths
- May add bootstrap-specific validation for config file creation, required directories, and runtime image/container readiness
