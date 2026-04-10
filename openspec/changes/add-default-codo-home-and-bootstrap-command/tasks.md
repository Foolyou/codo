## 1. Config Discovery And Default Home

- [x] 1.1 Add shared helpers for resolving `CODO_HOME`, `CODO_CONFIG`, and the default `~/.codo/config/runtime.json` path for config-backed CLI commands
- [x] 1.2 Add starter-config generation for the default home layout, including `config/`, `workspace/`, and config-relative state/log paths without overwriting an existing config
- [x] 1.3 Add unit tests covering config-path precedence, `CODO_HOME` overrides, and first-run default-home initialization behavior

## 2. Unified Bootstrap Command

- [x] 2.1 Add a top-level `codo up` command that uses shared config discovery and rejects missing custom config paths instead of auto-creating them
- [x] 2.2 Wire `codo up` to ensure the runtime image is available, start the host control plane, wait for control-plane readiness, and create or start the runtime container from the same config
- [x] 2.3 Add command-level or package-level tests covering bootstrap orchestration, including reuse of existing config and runtime bring-up from one invocation

## 3. Compatibility And Operator Flow

- [x] 3.1 Update existing config-backed commands to use shared config discovery while preserving explicit `--config` workflows
- [x] 3.2 Verify that the existing low-level commands (`control-plane serve`, `runtime build-image`, `runtime start`, `runtime reconnect`) remain usable alongside `codo up`
- [x] 3.3 Update any example config/template assets so the documented defaults no longer depend on repo-relative example paths

## 4. Documentation And Validation

- [x] 4.1 Rewrite the README deployment flow around the default `.codo` home layout and the `codo up` entrypoint
- [x] 4.2 Extend automated validation or smoke coverage for first-run bootstrap, runtime startup, and reconnect behavior under the new default layout
