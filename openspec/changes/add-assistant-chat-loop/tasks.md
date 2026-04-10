## 1. Assistant Command Surface

- [x] 1.1 Add `assistant` CLI subcommands for host-side `chat` and container-side `repl`, including the session and config plumbing they require
- [x] 1.2 Refactor startup orchestration so `assistant chat` can reuse healthy control-plane sockets or start a session-scoped control plane before attaching to the runtime
- [x] 1.3 Add runtime attachment logic that launches `codo assistant repl` inside the running container with an interactive TTY and a stable `session_id`

## 2. Container-side REPL and Chat Loop

- [x] 2.1 Implement the container-side assistant REPL with line-based input and `/help`, `/reset`, and `/exit` controls
- [x] 2.2 Implement in-memory conversation state and proxy-backed chat completions requests for each REPL session
- [x] 2.3 Register the v1 `bash` tool using the standard OpenAI-compatible `tools` schema and process `tool_calls` until a final assistant message is available
- [x] 2.4 Enforce per-turn tool-call limits and convert malformed or unsupported tool calls into failing tool results instead of crashing the session

## 3. Audited Bash Tool Integration

- [x] 3.1 Refactor the audited bash runtime path into a reusable executor that can return structured command results as well as terminal passthrough output
- [x] 3.2 Implement the assistant `bash` tool so commands run with a workspace-scoped working directory and bounded captured output
- [x] 3.3 Feed bash execution results back into the chat loop as `tool` messages while preserving audit correlation through runtime and session identifiers

## 4. Validation and Operator Docs

- [x] 4.1 Add tests for assistant CLI routing, lifecycle reuse or startup, REPL session controls, tool-call handling, and bash tool safety bounds
- [x] 4.2 Update README and operator documentation for `codo assistant chat`, `codo assistant repl`, current v1 behavior, and known limits
