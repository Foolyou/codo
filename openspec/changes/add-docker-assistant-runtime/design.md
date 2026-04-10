## Context

This change defines the first usable runtime for a personal AI assistant that can operate on a real project workspace. The runtime must be practical enough for daily use, which means the assistant needs shell access and a persistent execution environment, but the design must still keep the host system and upstream API credentials outside the assistant's direct trust boundary.

The chosen operating model is a long-lived assistant container running under rootless Docker. A single explicit workspace is mounted into the container and becomes the assistant's working area. Model traffic is sent to a host-side proxy so that the container never receives the real upstream token. Shell activity is audited by sending execution events from the container runtime to a host-side collector.

## Goals / Non-Goals

**Goals:**
- Run the assistant in a long-lived rootless Docker container instead of directly on the host.
- Expose `bash` as a first-class assistant tool inside the container.
- Mount only an explicitly selected workspace into the container for assistant read/write access.
- Keep upstream AI credentials on the host by routing model traffic through a host-side proxy.
- Support Alibaba Cloud Bailian pay-as-you-go model APIs as the only upstream model provider in v1, using the provider's OpenAI-compatible interface.
- Persist bash audit records on the host through a dedicated audit interface.
- Keep the runtime rebuildable so a persistent container does not become the source of truth.

**Non-Goals:**
- Network traffic monitoring, allowlists, denylists, or strict egress enforcement.
- General host secret management beyond keeping the upstream AI token outside the container.
- Human approval workflows for shell execution or file writes.
- Multi-workspace orchestration or dynamic remounting during an active container lifetime.
- Multi-provider model support or support for non-OpenAI-compatible upstream protocols in v1.
- Full host-level provenance for every file change independent of runtime-reported audit events.

## Decisions

### 1. Use a long-lived rootless Docker container as the assistant runtime

The assistant SHALL run inside a container managed by rootless Docker and intended to remain available across user sessions. This favors low friction and fast reuse over the stronger cleanliness of per-task ephemeral containers.

Why this approach:
- The user explicitly wants a long-lived environment.
- Reusing the same container reduces startup overhead and keeps shell tooling warm.
- Rootless Docker lowers the blast radius on the host compared with a privileged daemon-backed runtime.

Alternatives considered:
- Short-lived per-task containers: cleaner state isolation, but poorer ergonomics and higher startup churn.
- Direct host execution: simplest operationally, but defeats the isolation goal.

### 2. Treat the container as persistent but rebuildable

The container may accumulate packages, caches, and runtime state while it is running, but it SHALL remain replaceable. For v1, the system SHALL NOT add a separate long-lived runtime-state mount outside `/workspace`. Persistent truth lives outside the container in the mounted workspace and host-side control services, not in the container image, an auxiliary runtime volume, or the internal container filesystem.

Why this approach:
- Long-lived containers inevitably drift.
- Rebuildability keeps operational recovery simple when the runtime becomes polluted or misconfigured.

Alternatives considered:
- Fully immutable runtime with no persistent state: cleaner, but conflicts with the requirement for a long-lived daily driver.
- Fully stateful runtime as primary storage: convenient, but too fragile and hard to recover.

### 3. Keep upstream model credentials on the host via a dedicated AI proxy

The assistant container SHALL call a host-side AI proxy rather than the upstream model provider directly. The proxy injects the real token, forwards the request, and records request-level audit metadata outside the container.

Why this approach:
- The container remains usable without ever seeing the true upstream credential.
- Credential rotation and provider changes stay outside the assistant runtime boundary.
- Request-level logging is centralized in one host-side service.

Alternatives considered:
- Passing the token via environment variable or mounted file: operationally simple, but defeats the token isolation goal.
- Embedding a provider client directly into the runtime with host secrets mounted read-only: still leaks credential material into the container trust boundary.

### 4. Scope v1 model support to Bailian pay-as-you-go APIs over the OpenAI-compatible interface

The v1 system SHALL support a single upstream provider target: Alibaba Cloud Bailian pay-as-you-go model APIs using the provider's OpenAI-compatible API surface. The configured provider credentials and base URL remain provider-specific and host-managed, while the proxy keeps a clean internal boundary for future provider and protocol adapters.

Why this approach:
- It matches the user's first-phase provider choice and keeps scope narrow.
- OpenAI-compatible access reduces integration friction for the first milestone.
- A proxy adapter boundary leaves room for future providers or protocol families without reopening runtime isolation and audit decisions.

Alternatives considered:
- Supporting multiple providers in v1: more flexible, but expands testing and configuration complexity too early.
- Supporting Bailian's Anthropic-compatible surface in parallel: useful later, but unnecessary for the first milestone.
- Binding the runtime directly to a single provider wire format with no abstraction: simpler short term, but makes future expansion expensive.

### 5. Expose shell execution only through an audited wrapper

The assistant SHALL not execute raw shell commands directly. The runtime instead routes each bash invocation through a wrapper that sends structured `start` and `end` events to a host-side audit collector and only executes if the collector acknowledges the event channel.

Minimum event fields:
- `exec_id`
- `runtime_instance_id`
- `session_id`
- `container_id`
- `workspace_id` or workspace path label
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

Why this approach:
- Shell access is the most powerful assistant capability in scope.
- Requiring audit acknowledgement prevents silent unlogged execution.
- Structured events are easier to search and reason about than free-form shell history.

Alternatives considered:
- Post-hoc shell history scraping: incomplete and easy to bypass.
- Best-effort logging with fail-open execution: more available, but weakens the audit guarantee too much for this capability.
- Content-addressed chunk storage for full stdout/stderr in v1: more complete, but unnecessary complexity for the first milestone.

### 6. Mount a single explicit workspace into the runtime

The runtime SHALL mount only a user-selected long-lived workspace into the container and expose it at a stable path such as `/workspace`. The container SHALL not receive broad host mounts such as the user's full home directory, SSH configuration, or host-level configuration directories as part of this change.

Why this approach:
- The workspace mount defines the assistant's intended file authority.
- A single stable mount keeps the mental model simple for both users and runtime code.
- Limiting mounts is more reliable than trying to constrain behavior purely in prompts.

Alternatives considered:
- Mounting the user's home directory and relying on policy text: easiest to wire up, but too broad.
- Dynamic per-command mounts: more precise, but too much operational complexity for the first milestone.

### 7. Use host-local Unix domain sockets for control services

The host-side AI proxy and host-side audit collector SHALL be exposed to the container through host-local Unix domain sockets rather than general-purpose TCP listeners for v1.

Why this approach:
- These services are only meant to be consumed by the local assistant runtime.
- Unix domain sockets reduce accidental host exposure and simplify local trust boundaries.
- They align well with the goal of keeping the control plane minimal and host-bound.

Alternatives considered:
- Listening on host TCP ports: simpler for generic clients, but unnecessarily broad for a local-only control surface.

### 8. Implement v1 components in Go

The v1 host control plane and container-side runtime helper components SHALL be implemented in Go. This establishes a single implementation language for the first milestone without freezing third-party library choices before implementation begins.

Why this approach:
- A single systems-oriented language keeps the host daemon and runtime helper consistent.
- Go is well suited to long-lived daemons, Unix domain sockets, subprocess control, and structured logging.
- Delaying third-party library decisions keeps the design focused on architectural commitments instead of premature implementation detail.

Alternatives considered:
- Mixing languages between host services and runtime helpers: workable, but adds operational and maintenance overhead too early.
- Deferring language choice entirely to apply time: flexible, but leaves too much implementation-critical uncertainty for a cross-cutting runtime design.

### 9. Store audit records as append-only JSONL in v1

Audit persistence for model proxy records and bash execution records SHALL use append-only JSONL files on the host in the first milestone. Records are rolled by time or file size, but the storage model remains append-only.

Why this approach:
- It keeps implementation and debugging simple.
- The log format is easy to inspect directly without additional tooling.
- It is sufficient for the expected early-stage log volume.

Alternatives considered:
- SQLite or another structured store from day one: better queryability, but unnecessary operational and schema complexity for the first milestone.

## Risks / Trade-offs

- [Long-lived container state drift] → Keep the runtime rebuildable and avoid storing source-of-truth data inside the container.
- [Rebuild drops container-local packages and caches] → Treat interactive container mutations as disposable and move durable tooling into the image build.
- [Audit collector outage blocks shell access] → Fail closed by design; provide clear operator diagnostics and restart procedures.
- [Workspace mount still allows destructive edits within that directory] → Make the workspace explicit and narrow; do not imply safety inside the mounted path.
- [Rootless Docker is not a complete security boundary] → Treat this as blast-radius reduction, not absolute containment.
- [Proxy audit and command audit are split across services] → Use stable identifiers such as session, container, and workspace IDs to correlate records.
- [Preview-only stdout/stderr audit loses full command output] → Persist hashes, byte counts, and truncation flags so operators can still detect large or missing output.
- [JSONL becomes awkward to query at scale] → Keep schemas stable so the system can migrate to structured storage later without changing runtime behavior.
- [Bailian OpenAI-compatible support varies by deployed endpoint and model set] → Keep the base URL host-configurable and validate provider/model compatibility in proxy configuration and integration tests.

## Migration Plan

1. Introduce host-side control services for AI proxying and audit collection.
2. Add the long-lived rootless Docker runtime configuration and mount the selected workspace.
3. Route assistant model traffic through the host proxy and verify the container has no direct upstream credential.
4. Replace direct shell execution with the audited bash wrapper.
5. Validate rebuild flow by recreating the container without losing workspace data or host-side audit logs.

Rollback:
- Stop routing requests through the new runtime and proxy.
- Shut down and remove the assistant container.
- Retain host-side logs for investigation.
- Fall back to the prior local development workflow if one exists.

## Open Questions

- When should audit storage move from JSONL to a structured store such as SQLite?
- Should a future version add host-side file change observation to complement runtime-reported bash audit events?
