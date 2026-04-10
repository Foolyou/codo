## Why

The runtime foundation is in place, but the project still lacks the first end-user assistant experience: there is no assistant command, no interactive conversation loop, and no mechanism that turns model output into audited actions inside the runtime. The next step is to make the assistant actually usable from a terminal while preserving the existing container, proxy, and audit trust boundaries.

## What Changes

- Add a container-local `codo assistant repl` entrypoint that runs an interactive assistant session inside the long-lived runtime container.
- Add a host-side `codo assistant chat` command that ensures the runtime is reachable and attaches the operator terminal to the container-local REPL without requiring a manual shell hop.
- Implement a chat-completions loop inside the runtime that preserves per-session conversation state and uses the standard OpenAI-compatible `tools` / `tool_calls` protocol.
- Register a minimal `bash` tool for v1 and route every tool execution through the existing audited bash path inside the runtime.
- Bound per-turn tool execution so malformed or runaway tool loops fail clearly instead of hanging the session.
- Keep v1 interaction to a line-oriented REPL and exclude full-screen TUI work, persisted chat transcripts, and broader tool/plugin ecosystems from this change.

## Capabilities

### New Capabilities
- `assistant-chat-interface`: Provide a container-local assistant REPL and a host-side shortcut command that attaches the operator terminal to it.
- `assistant-tool-call-loop`: Execute assistant turns through the proxy-backed chat completions loop and handle standard `tool_calls` with an audited `bash` tool.

### Modified Capabilities

None.

## Impact

- Adds a new `assistant` command surface to the `codo` CLI on both the host side and inside the runtime container.
- Introduces assistant-session and tool-call orchestration code while reusing the existing runtime, proxy, and bash audit components.
- Extends the runtime workflow from infrastructure-only operations to an operator-facing assistant interaction path.
- Keeps the provider boundary and audit boundary intact by executing the loop inside the container and sending model traffic through the host-side proxy.
