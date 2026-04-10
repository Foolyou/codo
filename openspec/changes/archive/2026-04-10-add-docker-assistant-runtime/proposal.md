## Why

The project needs a practical first step toward a personal AI assistant that can operate on a real workspace without exposing the host system or upstream API tokens directly to the assistant runtime. The immediate goal is to make the assistant usable day to day by giving it shell access inside a long-lived rootless Docker container while keeping token injection and audit collection on the host.

## What Changes

- Add a long-lived assistant runtime that runs inside a rootless Docker container and exposes `bash` as a first-class tool for the assistant.
- Mount only an explicitly selected workspace directory into the container for assistant read/write operations.
- Add a host-side AI proxy that receives model requests from the container, injects upstream credentials on the host, and forwards requests to the real AI service.
- Limit v1 upstream model support to Alibaba Cloud Bailian pay-as-you-go model APIs using the provider's OpenAI-compatible interface and host-managed provider credentials.
- Keep the model proxy boundary extensible so future versions can add other providers and protocol families without redesigning the runtime trust boundary.
- Add a host-side command audit interface that receives `bash` execution events from the container runtime and persists them as audit records.
- Define reconstruction and failure behavior for the long-lived container so the runtime remains replaceable even though it is persistent.
- Exclude network monitoring, network allow/deny policy, and full egress enforcement from this change.

## Capabilities

### New Capabilities
- `assistant-runtime`: Run the assistant inside a long-lived rootless Docker container with a mounted workspace and shell execution support.
- `assistant-model-proxy`: Forward model requests through a host-side proxy that injects real credentials outside the container.
- `assistant-command-audit`: Record assistant shell activity through a host-side audit interface fed by the container runtime.

### Modified Capabilities

None.

## Impact

- Adds a persistent assistant container lifecycle managed through rootless Docker.
- Introduces a host-side control surface for model proxying and audit ingestion.
- Constrains v1 model access to Alibaba Cloud Bailian pay-as-you-go OpenAI-compatible model APIs.
- Defines workspace mounting conventions and trust boundaries between host and container.
- Establishes the initial OpenSpec contract for runtime isolation, credential handling, and shell audit behavior.
