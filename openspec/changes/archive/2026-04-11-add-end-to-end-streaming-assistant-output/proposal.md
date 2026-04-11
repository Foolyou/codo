## Why

The assistant loop is usable today, but every turn waits for a fully buffered completion before the user sees anything. That makes long answers and tool-heavy turns feel slow, hides progress during upstream generation, and blocks the project from offering the interactive terminal experience the current runtime architecture is meant to support.

## What Changes

- Add end-to-end streaming for assistant turns so the runtime can request streamed chat completions and render assistant text incrementally in the REPL.
- Preserve proxy-mediated streaming across the host-side model proxy instead of depending on fully buffered completion bodies.
- Define how the assistant turn engine accumulates streamed content, recognizes streamed tool calls, and transitions from streaming model output into the existing audited tool loop.
- Define clear behavior for stream interruption, malformed streamed events, and providers or environments that cannot complete a streaming turn successfully.
- Update operator documentation and validation coverage for streamed terminal behavior, tool-call turns, and error handling without changing the existing runtime trust boundary.

## Capabilities

### New Capabilities
- `assistant-streaming-output`: Stream assistant turn output and streamed tool-call payloads into the line-oriented REPL while preserving correct session state and tool execution semantics.

### Modified Capabilities
- `assistant-model-proxy`: Ensure the proxy can forward streaming chat-completions responses to the runtime without buffering away incremental upstream output or bypassing existing audit requirements.

## Impact

- Affects the container-side assistant turn engine in [`internal/runtime/assistant.go`](/home/chenan/projects/codo/internal/runtime/assistant.go) and related runtime tests.
- Affects proxy request and response handling in [`internal/controlplane/proxy.go`](/home/chenan/projects/codo/internal/controlplane/proxy.go) and the runtime-side proxy client in [`internal/runtime/bash.go`](/home/chenan/projects/codo/internal/runtime/bash.go).
- Updates the operator-facing behavior documented in [`README.md`](/home/chenan/projects/codo/README.md).
- Keeps the current container runtime, host-side credential injection, and audited bash tool boundaries intact.
