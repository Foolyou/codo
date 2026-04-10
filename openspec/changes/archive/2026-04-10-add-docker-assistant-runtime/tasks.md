## 1. Runtime Foundation

- [x] 1.1 Define the Go-based v1 runtime and host control-plane structure, including workspace mount path, Unix socket paths for the proxy and audit collector, and the `runtime_instance_id` / `session_id` model
- [x] 1.2 Create the rootless Docker image and Go-based container lifecycle tooling needed to start, stop, rebuild, and reconnect to the assistant runtime
- [x] 1.3 Implement workspace mounting so the configured long-lived workspace is exposed at a stable in-container path, broad host mounts are excluded, and rebuilds do not depend on any extra long-lived runtime-state volume
- [x] 1.4 Wire the assistant runtime so bash commands execute inside the container instead of on the host

## 2. Host-side Model Proxy

- [x] 2.1 Implement the host-side AI proxy as a host-local Unix socket service that accepts runtime requests and forwards them to Alibaba Cloud Bailian's pay-as-you-go model APIs through the provider's OpenAI-compatible interface
- [x] 2.2 Add host-side credential injection and provider-specific configuration so the Bailian API key and OpenAI-compatible base URL remain outside the assistant container
- [x] 2.3 Persist request-level proxy audit metadata as append-only JSONL records correlated by `request_id`, `runtime_instance_id`, `session_id`, and workspace context
- [x] 2.4 Enforce proxy-only model access so runtime requests fail instead of bypassing the proxy when the proxy is unavailable
- [x] 2.5 Structure the proxy so future providers or protocol families can be added without changing the runtime trust boundary or moving credentials into the container

## 3. Bash Audit Pipeline

- [x] 3.1 Implement the container-side bash wrapper that emits structured start and completion events for every assistant shell invocation
- [x] 3.2 Implement the host-side audit collector as a host-local Unix socket service that accepts bash execution events and persists them outside the container
- [x] 3.3 Ensure persisted audit records are append-only JSONL records that include command, cwd, timing, exit code, `runtime_instance_id`, `session_id`, workspace correlation fields, bounded stdout/stderr previews, byte counts, hashes, and truncation flags
- [x] 3.4 Enforce fail-closed behavior so assistant bash execution is rejected when the audit collector is unavailable

## 4. Integration and Validation

- [x] 4.1 Validate that the long-lived runtime can be restarted or rebuilt from a clean container state without losing the mounted workspace or host-side audit history
- [x] 4.2 Validate that the assistant container does not require direct access to the upstream AI token during normal operation
- [x] 4.3 Add operator documentation covering runtime startup, rebuild flow, workspace selection, and audit log inspection
