## ADDED Requirements

### Requirement: Shared runtime config discovery
The system SHALL resolve the runtime config path for config-backed operator commands using this precedence order: explicit `--config`, then `CODO_CONFIG`, then `<codo-home>/config/runtime.json`, where `<codo-home>` SHALL resolve from `CODO_HOME` when set and otherwise from `$HOME/.codo`.

#### Scenario: Use default home config path
- **WHEN** an operator runs a config-backed `codo` command without `--config`, without `CODO_CONFIG`, and without `CODO_HOME`
- **THEN** the system SHALL attempt to use `$HOME/.codo/config/runtime.json`

#### Scenario: Prefer explicit config flag
- **WHEN** an operator provides `--config /path/to/runtime.json`
- **THEN** the system SHALL use `/path/to/runtime.json` instead of `CODO_CONFIG` or the default home config path

#### Scenario: Prefer config environment override
- **WHEN** an operator omits `--config` and sets `CODO_CONFIG` to a custom config path
- **THEN** the system SHALL use the `CODO_CONFIG` path instead of the default home config path

#### Scenario: Prefer custom CODO home for default path resolution
- **WHEN** an operator omits `--config`, does not set `CODO_CONFIG`, and sets `CODO_HOME` to a custom directory
- **THEN** the system SHALL attempt to use `<CODO_HOME>/config/runtime.json` as the default config path

### Requirement: Default `.codo` home bootstrap layout
The unified bootstrap flow SHALL treat the resolved `<codo-home>` directory as the default local deployment root and SHALL prepare `config/` and `workspace/` as the default user-facing subdirectories when bootstrapping an uninitialized default home.

#### Scenario: Bootstrap creates default home assets
- **WHEN** an operator runs the unified bootstrap command against the default home path and `<codo-home>/config/runtime.json` does not exist
- **THEN** the system SHALL create `<codo-home>/config/`, `<codo-home>/workspace/`, and a starter runtime config file under `<codo-home>/config/runtime.json`

#### Scenario: Default config points at the default workspace
- **WHEN** the system creates the starter runtime config for the default home path
- **THEN** the resolved runtime workspace path SHALL point to `<codo-home>/workspace` and SHALL NOT default to a repo-relative example location

#### Scenario: Bootstrap preserves existing config
- **WHEN** an operator reruns the unified bootstrap command after `<codo-home>/config/runtime.json` already exists
- **THEN** the system SHALL reuse the existing config and SHALL NOT overwrite the operator's file automatically

### Requirement: Unified control-plane and runtime bring-up
The system SHALL provide a single operator command that brings up the host control plane and ensures the assistant runtime container is running from the same resolved config.

#### Scenario: Start control plane and runtime together
- **WHEN** an operator runs the unified bootstrap command with a valid resolved config
- **THEN** the system SHALL start the configured host control-plane listeners and SHALL create or start the configured assistant runtime container during the same command invocation

#### Scenario: Ensure runtime image availability during bootstrap
- **WHEN** an operator runs the unified bootstrap command and the configured runtime image is not available locally
- **THEN** the system SHALL make the configured image available before attempting to create the assistant runtime container

#### Scenario: Reject missing custom config during bootstrap
- **WHEN** an operator runs the unified bootstrap command with an explicit `--config` path or `CODO_CONFIG` path that does not exist
- **THEN** the system SHALL fail with a clear error instead of creating files at that custom path automatically
