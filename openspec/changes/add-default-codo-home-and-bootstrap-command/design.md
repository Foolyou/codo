## Context

The current runtime flow is optimized for development inside this repository: every config-backed command defaults to `examples/runtime-config.example.json`, the README asks operators to copy and edit a local config file, and bringing the system up requires separate `control-plane serve`, `runtime build-image`, and `runtime start` steps. That works for an initial implementation but is awkward for deployment because the default path is tied to the repo checkout and the operator workflow is fragmented across multiple commands.

This change introduces an operator-facing bootstrap layer without changing the existing runtime trust boundary. The assistant container still mounts only the configured workspace directory, the control plane still owns the proxy and audit sockets on the host, and repo-local explicit configs must remain supported.

## Goals / Non-Goals

**Goals:**
- Give the runtime a stable default home rooted at `~/.codo` for local deployments.
- Define a predictable default layout with user-facing `config/` and `workspace/` directories.
- Add one first-class command that can bring up the control plane and runtime container together from the same resolved config.
- Reuse the existing runtime and control-plane implementation rather than introducing a parallel deployment path.
- Keep explicit `--config` workflows working for operators who do not want the default home layout.

**Non-Goals:**
- Replacing JSON config with another format or config backend.
- Adding multi-workspace orchestration in this change.
- Changing the assistant container's workspace-only mount policy.
- Adding daemonization, service-file generation, or process supervision beyond a foreground bootstrap command.
- Removing existing low-level commands such as `control-plane serve` or `runtime start`.

## Decisions

### 1. Introduce shared config discovery with explicit precedence

All config-backed host commands will resolve their config path through one shared helper with this order:

1. `--config <path>` when explicitly provided
2. `CODO_CONFIG`
3. `<codo-home>/config/runtime.json`

`<codo-home>` resolves from `CODO_HOME` when present and otherwise from `$HOME/.codo`.

Why this approach:
- It removes the repo-relative `examples/...` default from normal operator workflows.
- It keeps deployment flexible for service accounts and nonstandard locations.
- A single resolution path avoids per-command drift in defaults.

Alternatives considered:
- Keep repo-local example paths as defaults: simple, but still poor for deployment.
- Hardcode only `~/.codo` with no override: easier to document, but too rigid for noninteractive or multi-user setups.
- Adopt XDG directories immediately: viable long term, but it does not match the requested `.codo` entrypoint and adds extra migration complexity now.

### 2. Make `~/.codo/config/runtime.json` the default config and `~/.codo/workspace` the default mounted workspace

The bootstrap flow will materialize a starter config at `~/.codo/config/runtime.json`. That config will set its workspace path relative to the config file so the default mounted workspace resolves to `~/.codo/workspace`. Generated runtime state, sockets, and logs will live under `~/.codo/config/state` by default so this change can keep `config/` and `workspace/` as the only required top-level user-facing directories.

Why this approach:
- It matches the requested home layout closely.
- It fits the existing `config.Load` behavior, which already resolves relative paths from the config file directory.
- It avoids forcing operators to keep runtime state under a repo checkout.

Alternatives considered:
- Add a top-level `~/.codo/state` directory: cleaner separation, but expands the visible layout beyond the requested `config/` and `workspace/` structure.
- Keep using `./.runtime` relative to the current working directory: familiar for development, but wrong for deployment.
- Put the config directly at `~/.codo/runtime.json`: fewer directories, but weaker organization and less room for future config assets.

### 3. Add a built-in foreground `codo up` command instead of relying on an external bootstrap script

The preferred operator entrypoint will be a new top-level `codo up` command. It will:

- Resolve the config path through the shared discovery helper
- If the resolved path is the default home config and it does not exist yet, create `config/`, `workspace/`, and a starter config file
- Ensure the configured runtime image is available locally, building it only when it is missing
- Start the control plane in-process
- Wait until the host sockets are bound
- Ensure the runtime container is created or started
- Remain in the foreground until interrupted, shutting down the control plane through context cancellation

Why this approach:
- The bootstrap behavior stays inside the same binary and shares the same config and validation code.
- Operators get one documented command instead of a shell-script convention layered on top of core commands.
- A foreground command is easy to run directly during development and easy to supervise with systemd, tmux, or similar tools for deployment.

Alternatives considered:
- Provide only a shell script: faster to assemble, but duplicates path resolution and makes portability and testing worse.
- Always daemonize: more service-like, but it adds PID management, log routing, and lifecycle questions that are not needed for this change.
- Keep the current multi-command workflow and only improve docs: lower code churn, but it does not solve the fragmented bring-up problem.

### 4. Limit automatic initialization to the default home path

Only `codo up` using the discovered default config location will create missing directories and a starter config automatically. If an operator supplies a custom `--config` path or `CODO_CONFIG` points to a missing file, the command will fail with a clear error instead of writing to an arbitrary location.

Why this approach:
- It keeps first-run local setup friction low.
- It avoids surprising writes when an operator explicitly chose a custom config path.
- It preserves a clear distinction between "default bootstrap" and "custom managed config."

Alternatives considered:
- Auto-create any missing config path: convenient, but unsafe and too implicit.
- Never auto-create config: safer, but it weakens the value of the default bootstrap flow.

### 5. Reuse existing low-level runtime commands and keep bootstrap non-destructive

`codo up` will orchestrate existing control-plane and runtime behaviors instead of replacing them. Existing commands like `control-plane serve`, `runtime build-image`, and `runtime start` remain available. `codo up` will not overwrite an existing config file, will not force a rebuild on every launch, and will not remove an existing container as part of normal startup.

Why this approach:
- It minimizes architectural churn and preserves manual escape hatches.
- Operators can still debug failures one layer at a time using the existing subcommands.
- Non-destructive bootstrap behavior is safer for repeated daily use.

Alternatives considered:
- Collapse the old commands into the new one: simpler surface area, but worse debuggability.
- Force a rebuild on every `up`: more deterministic, but too slow and disruptive for a default path.

## Risks / Trade-offs

- [First-run auto-initialization may hide where files came from] → Restrict creation to the default home path and document the created files explicitly.
- [Foreground `codo up` still needs an external supervisor for unattended deployment] → Treat service supervision as an operator concern for now and document that `codo up` is service-manager friendly.
- [Keeping generated state under `config/state` mixes operator config and runtime artifacts] → Scope mutable files under a dedicated subdirectory and keep the root layout minimal for this change.
- [Build-if-missing can turn a startup problem into a Docker build failure] → Surface build logs directly and preserve manual `runtime build-image` as a separate recovery path.
- [Changing default config resolution affects every config-backed command] → Preserve explicit `--config` and environment overrides so existing setups can continue unchanged.

## Migration Plan

1. Add config discovery helpers for `CODO_HOME`, `CODO_CONFIG`, and the default `~/.codo/config/runtime.json` path.
2. Add starter-config generation for the default home layout, including `~/.codo/workspace` and config-relative state/log paths.
3. Update config-backed CLI commands to use shared config discovery instead of repo-local example defaults.
4. Add `codo up` orchestration that starts the control plane, waits for readiness, and ensures the runtime image and container are available.
5. Update README and examples to document the default home layout and the new bootstrap flow.

Rollback:
- Continue using explicit `--config` paths with existing repo-local config files.
- Use the existing `control-plane serve`, `runtime build-image`, and `runtime start` commands directly if the new bootstrap flow is not desired.
- Leave `~/.codo` unused or remove it manually if an operator wants to revert to a fully custom layout.

## Open Questions

- Should a follow-up change add `codo init` and `codo down`, or is `codo up` plus the existing low-level commands sufficient?
- Should a later deployment-focused change move generated state out of `config/state` into a separate top-level directory once the default home layout is established?
